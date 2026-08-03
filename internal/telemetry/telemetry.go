// Package telemetry sends anonymous, aggregate product-usage telemetry to
// Aptabase (https://aptabase.com) to help prioritize maintenance of this
// open-source project.
//
// ON BY DEFAULT. Opt out with either DO_NOT_TRACK=1 (the community
// convention, see https://consoledonottrack.com) or OPCUA_MCP_TELEMETRY=false.
//
// What is collected, once per flush interval (default 10 minutes, one HTTP
// request per interval - never one per MCP call):
//   - Counts of MCP tool calls, by tool name (e.g. "opcua_read": 42)
//   - Average number of parameters per tool call (a count, never the values)
//   - Cache hit/miss counts
//   - Auth mode in use (anonymous/username/certificate - the mode name only)
//   - Security policy in use (e.g. "Basic256Sha256" - the config enum value)
//   - Discovered node count, bucketed into ranges (<100, 100-1k, 1k-10k,
//     >10k) - never the exact count
//   - Error counts by coarse category (e.g. "timeout", "type_mismatch") -
//     never raw error message text; see ErrKind* constants in aggregator.go
//     for the full closed set, which RecordError enforces by construction
//   - Transport mode (stdio/http), server version, OS, arch, session
//     duration
//   - A random anonymous ID persisted to
//     $XDG_CONFIG_HOME/opcua-mcp/telemetry_id, not derived from or linkable
//     to any hardware or network identifier
//
// What is never collected, under any circumstance:
//   - Node IDs, browse names, node paths
//   - OPC-UA endpoint URLs, hostnames, IP addresses
//   - Any value read from or written to a node
//   - Raw error message text
//   - Usernames, passwords, certificate contents or paths
//   - Raw OPC-UA server ApplicationName/ProductUri strings
//
// See buildPayload below for the exact JSON shape sent, and
// telemetry_test.go's TestSessionSummaryPayloadAllowlist for the test that
// enforces this list can never silently grow.
package telemetry

import (
	"context"
	"runtime"
	"sync"
	"time"
)

// Telemetry is the interface every call site uses. Its no-op implementation
// (returned by New when telemetry is disabled) makes every method a cheap
// no-op, so callers never need an `if telemetry.Enabled()` check scattered
// through tool handlers, the caching client, or anywhere else.
type Telemetry interface {
	// RecordToolCall records one MCP tool invocation.
	RecordToolCall(name string, paramCount int)
	// RecordCacheResult records one read-through cache lookup outcome.
	RecordCacheResult(hit bool)
	// RecordError records one error, classified into a coarse category (use
	// the ErrKind* constants) - never a raw error message.
	RecordError(kind string)
	// SetDiscoveryStats updates the current discovered-node gauge.
	SetDiscoveryStats(nodeCount int)
	// Start begins the periodic flush loop in the background, returning
	// immediately. It flushes once more when ctx is canceled.
	Start(ctx context.Context)
	// Close waits for the flush loop (and its final flush) to finish. Call
	// it only after the ctx passed to Start has already been canceled.
	Close()
}

// New constructs a Telemetry. If cfg.Enabled is false, it returns a no-op
// implementation and nothing is ever sent.
func New(cfg Config) Telemetry {
	if !cfg.Enabled {
		return noopTelemetry{}
	}
	return &liveTelemetry{
		cfg:       cfg,
		agg:       newAggregator(),
		http:      newHTTPClient(),
		startedAt: time.Now(),
	}
}

// liveTelemetry is the real implementation, active when telemetry is
// enabled.
type liveTelemetry struct {
	cfg       Config
	agg       *aggregator
	http      *httpClient
	startedAt time.Time
	wg        sync.WaitGroup
}

func (t *liveTelemetry) RecordToolCall(name string, paramCount int) {
	t.agg.recordToolCall(name, paramCount)
}

func (t *liveTelemetry) RecordCacheResult(hit bool) {
	t.agg.recordCacheResult(hit)
}

func (t *liveTelemetry) RecordError(kind string) {
	t.agg.recordError(kind)
}

func (t *liveTelemetry) SetDiscoveryStats(nodeCount int) {
	t.agg.setDiscoveryStats(nodeCount)
}

func (t *liveTelemetry) Start(ctx context.Context) {
	t.wg.Add(1)
	go t.run(ctx)
}

func (t *liveTelemetry) run(ctx context.Context) {
	defer t.wg.Done()

	ticker := time.NewTicker(t.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.flush()
		case <-ctx.Done():
			t.flush()
			return
		}
	}
}

// flush snapshots the aggregator and sends the resulting event in its own
// goroutine (tracked by wg so Close can wait for it), so a slow or hanging
// send never delays the flush loop itself.
func (t *liveTelemetry) flush() {
	snap := t.agg.snapshot()
	payload := buildPayload(t.cfg, snap, time.Since(t.startedAt))

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.http.send(payload)
	}()
}

func (t *liveTelemetry) Close() {
	t.wg.Wait()
}

// noopTelemetry is returned by New when telemetry is disabled (or DO_NOT_TRACK
// / OPCUA_MCP_TELEMETRY opted out). Every method is a no-op safe to call with
// any input, including zero values.
type noopTelemetry struct{}

func (noopTelemetry) RecordToolCall(string, int) {}
func (noopTelemetry) RecordCacheResult(bool)     {}
func (noopTelemetry) RecordError(string)         {}
func (noopTelemetry) SetDiscoveryStats(int)      {}
func (noopTelemetry) Start(context.Context)      {}
func (noopTelemetry) Close()                     {}

// eventPayload is the exact JSON body POSTed to Aptabase's event ingest API
// (POST https://us.aptabase.com/api/v0/event), matching the shape used by
// Aptabase's official SDKs.
type eventPayload struct {
	Timestamp   string                 `json:"timestamp"`
	SessionID   string                 `json:"sessionId"`
	EventName   string                 `json:"eventName"`
	SystemProps systemProps            `json:"systemProps"`
	Props       map[string]interface{} `json:"props"`
}

type systemProps struct {
	OSName     string `json:"osName"`
	AppVersion string `json:"appVersion"`
	SDKVersion string `json:"sdkVersion"`
}

// sdkVersion identifies this hand-rolled ingest client to Aptabase, in lieu
// of an official SDK dependency.
const sdkVersion = "opcua-mcp-telemetry/1"

// buildPayload assembles the single session_summary event sent per flush
// interval from the current config and aggregator snapshot. Every field here
// is deliberately drawn only from the allowed data model documented at the
// top of this file - see TestSessionSummaryPayloadAllowlist for the test
// that fails loudly if a field outside that list is ever added here.
func buildPayload(cfg Config, snap aggregatorSnapshot, sessionDuration time.Duration) eventPayload {
	props := map[string]interface{}{
		"anon_id":                  cfg.AnonID,
		"session_duration_seconds": int64(sessionDuration.Seconds()),
		"transport":                cfg.Transport,
		"arch":                     runtime.GOARCH,
		"auth_mode":                cfg.AuthMode,
		"security_policy":          cfg.SecurityPolicy,
		"discovered_node_bucket":   bucketNodeCount(snap.NodeCount),
		"cache_hits":               snap.CacheHits,
		"cache_misses":             snap.CacheMisses,
		"tool_call_count":          snap.ToolCallCount,
	}

	if snap.ToolCallCount > 0 {
		props["avg_params_per_call"] = float64(snap.TotalParams) / float64(snap.ToolCallCount)
	} else {
		props["avg_params_per_call"] = 0.0
	}
	if len(snap.ToolCalls) > 0 {
		props["tool_calls"] = snap.ToolCalls
	}
	if len(snap.ErrorCounts) > 0 {
		props["errors"] = snap.ErrorCounts
	}

	return eventPayload{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		SessionID: cfg.AnonID,
		EventName: "session_summary",
		SystemProps: systemProps{
			OSName:     runtime.GOOS,
			AppVersion: cfg.ServerVersion,
			SDKVersion: sdkVersion,
		},
		Props: props,
	}
}
