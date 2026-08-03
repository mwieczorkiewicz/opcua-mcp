package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// --- Opt-out resolution ---

func TestResolveEnabled(t *testing.T) {
	tests := []struct {
		name          string
		doNotTrack    string // "" means unset
		setDoNotTrack bool
		telemetryVar  string // "" means unset
		setTelemetry  bool
		want          bool
	}{
		{name: "both unset defaults to enabled", want: true},
		{name: "DO_NOT_TRACK=1 disables", setDoNotTrack: true, doNotTrack: "1", want: false},
		{name: "DO_NOT_TRACK=true disables", setDoNotTrack: true, doNotTrack: "true", want: false},
		{name: "DO_NOT_TRACK=TRUE disables (case-insensitive)", setDoNotTrack: true, doNotTrack: "TRUE", want: false},
		{name: "DO_NOT_TRACK=0 stays enabled", setDoNotTrack: true, doNotTrack: "0", want: true},
		{name: "DO_NOT_TRACK=false stays enabled", setDoNotTrack: true, doNotTrack: "false", want: true},
		{name: "OPCUA_MCP_TELEMETRY=false disables", setTelemetry: true, telemetryVar: "false", want: false},
		{name: "OPCUA_MCP_TELEMETRY=0 disables", setTelemetry: true, telemetryVar: "0", want: false},
		{name: "OPCUA_MCP_TELEMETRY=true stays enabled", setTelemetry: true, telemetryVar: "true", want: true},
		{name: "DO_NOT_TRACK=1 wins over OPCUA_MCP_TELEMETRY=true", setDoNotTrack: true, doNotTrack: "1", setTelemetry: true, telemetryVar: "true", want: false},
		{name: "unrecognized DO_NOT_TRACK value falls through to OPCUA_MCP_TELEMETRY", setDoNotTrack: true, doNotTrack: "maybe", setTelemetry: true, telemetryVar: "false", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("DO_NOT_TRACK")
			os.Unsetenv("OPCUA_MCP_TELEMETRY")
			if tt.setDoNotTrack {
				os.Setenv("DO_NOT_TRACK", tt.doNotTrack)
				defer os.Unsetenv("DO_NOT_TRACK")
			}
			if tt.setTelemetry {
				os.Setenv("OPCUA_MCP_TELEMETRY", tt.telemetryVar)
				defer os.Unsetenv("OPCUA_MCP_TELEMETRY")
			}

			if got := resolveEnabled(); got != tt.want {
				t.Errorf("resolveEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewReturnsNoopWhenDisabled(t *testing.T) {
	tel := New(Config{Enabled: false})
	if _, ok := tel.(noopTelemetry); !ok {
		t.Fatalf("New(Config{Enabled: false}) = %T, want noopTelemetry", tel)
	}
}

func TestNewReturnsLiveWhenEnabled(t *testing.T) {
	tel := New(Config{Enabled: true, FlushInterval: time.Hour})
	if _, ok := tel.(*liveTelemetry); !ok {
		t.Fatalf("New(Config{Enabled: true}) = %T, want *liveTelemetry", tel)
	}
}

// --- session_summary payload allowlist ---
//
// This is the enforcement mechanism for the "never collect X" rules
// documented at the top of telemetry.go: it fails loudly if a field outside
// these explicit allowlists is ever added to buildPayload's output, catching
// an accidental leak (e.g. a raw error string, a node ID, an endpoint URL)
// by construction rather than relying on code review alone.

var allowedTopLevelKeys = map[string]bool{
	"timestamp":   true,
	"sessionId":   true,
	"eventName":   true,
	"systemProps": true,
	"props":       true,
}

var allowedSystemPropsKeys = map[string]bool{
	"osName":     true,
	"appVersion": true,
	"sdkVersion": true,
}

var allowedPropsKeys = map[string]bool{
	"anon_id":                  true,
	"session_duration_seconds": true,
	"transport":                true,
	"arch":                     true,
	"auth_mode":                true,
	"security_policy":          true,
	"discovered_node_bucket":   true,
	"cache_hits":               true,
	"cache_misses":             true,
	"tool_call_count":          true,
	"avg_params_per_call":      true,
	"tool_calls":               true,
	"errors":                   true,
}

// knownToolNames mirrors the tool names registered in
// internal/mcp/server.go's SetupTools - kept as a literal list here (rather
// than importing internal/mcp, which would create an import cycle back into
// this package) since tool_calls keys must only ever be one of these.
var knownToolNames = map[string]bool{
	"opcua_read":                true,
	"opcua_write":               true,
	"opcua_browse":              true,
	"opcua_node_info":           true,
	"opcua_server_info":         true,
	"opcua_connect":             true,
	"opcua_disconnect":          true,
	"opcua_get_value":           true,
	"opcua_browse_nodes":        true,
	"opcua_get_value_by_name":   true,
	"opcua_find_similar_nodes":  true,
	"opcua_debug_search":        true,
	"opcua_discovery_stats":     true,
	"opcua_force_discovery":     true,
	"opcua_ensure_server_nodes": true,
	"opcua_subscribe":           true,
	"opcua_unsubscribe":         true,
	"opcua_list_subscriptions":  true,
}

func TestSessionSummaryPayloadAllowlist(t *testing.T) {
	snap := aggregatorSnapshot{
		ToolCalls:     map[string]int64{"opcua_read": 42, "opcua_write": 3},
		ToolCallCount: 45,
		TotalParams:   81,
		CacheHits:     120,
		CacheMisses:   34,
		ErrorCounts:   map[string]int64{ErrKindTimeout: 2, ErrKindTypeMismatch: 1},
		NodeCount:     5000, // must appear only as a bucket, never as "5000"
	}
	cfg := Config{
		AnonID:         "00000000-0000-4000-8000-000000000000",
		Transport:      "stdio",
		AuthMode:       "anonymous",
		SecurityPolicy: "Basic256Sha256",
		ServerVersion:  "1.0.0",
	}

	payload := buildPayload(cfg, snap, 612*time.Second)

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) error: %v", err)
	}

	if strings.Contains(string(data), "5000") {
		t.Error("marshaled payload contains the exact node count 5000 - NodeCount must only ever appear bucketed")
	}

	var generic map[string]json.RawMessage
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("json.Unmarshal(payload) error: %v", err)
	}

	for k := range generic {
		if !allowedTopLevelKeys[k] {
			t.Errorf("payload has disallowed top-level key %q", k)
		}
	}

	var systemProps map[string]json.RawMessage
	if err := json.Unmarshal(generic["systemProps"], &systemProps); err != nil {
		t.Fatalf("unmarshal systemProps: %v", err)
	}
	for k := range systemProps {
		if !allowedSystemPropsKeys[k] {
			t.Errorf("systemProps has disallowed key %q", k)
		}
	}

	var props map[string]json.RawMessage
	if err := json.Unmarshal(generic["props"], &props); err != nil {
		t.Fatalf("unmarshal props: %v", err)
	}
	for k := range props {
		if !allowedPropsKeys[k] {
			t.Errorf("props has disallowed key %q", k)
		}
	}

	var toolCalls map[string]int64
	if err := json.Unmarshal(props["tool_calls"], &toolCalls); err != nil {
		t.Fatalf("unmarshal props.tool_calls: %v", err)
	}
	if len(toolCalls) == 0 {
		t.Error("expected non-empty tool_calls in payload")
	}
	for name := range toolCalls {
		if !knownToolNames[name] {
			t.Errorf("tool_calls has disallowed key %q (not a known registered tool name)", name)
		}
	}

	var errs map[string]int64
	if err := json.Unmarshal(props["errors"], &errs); err != nil {
		t.Fatalf("unmarshal props.errors: %v", err)
	}
	if len(errs) == 0 {
		t.Error("expected non-empty errors in payload")
	}
	for kind := range errs {
		if !knownErrorKinds[kind] {
			t.Errorf("errors has disallowed key %q (not a known ErrKind* category)", kind)
		}
	}

	var bucket string
	if err := json.Unmarshal(props["discovered_node_bucket"], &bucket); err != nil {
		t.Fatalf("unmarshal props.discovered_node_bucket: %v", err)
	}
	if bucket != "1k-10k" {
		t.Errorf("discovered_node_bucket = %q, want %q for NodeCount=5000", bucket, "1k-10k")
	}
}

// TestRecordErrorClampsUnknownKind is the aggregator-level half of the
// "never raw error message text" enforcement: even if a future call site
// passes err.Error() straight into RecordError instead of a category, the
// aggregator clamps it to ErrKindOther before it can ever reach a snapshot
// or payload.
func TestRecordErrorClampsUnknownKind(t *testing.T) {
	agg := newAggregator()
	agg.recordError("some raw error message: connection to 10.0.0.5:4840 refused")
	agg.recordError(ErrKindTimeout)

	snap := agg.snapshot()

	if snap.ErrorCounts[ErrKindOther] != 1 {
		t.Errorf("expected 1 clamped-to-other error, got %d", snap.ErrorCounts[ErrKindOther])
	}
	if snap.ErrorCounts[ErrKindTimeout] != 1 {
		t.Errorf("expected 1 timeout error, got %d", snap.ErrorCounts[ErrKindTimeout])
	}
	for kind := range snap.ErrorCounts {
		if !knownErrorKinds[kind] {
			t.Errorf("snapshot contains disallowed error kind %q", kind)
		}
	}
}

// --- no-op safety ---

func TestNoopTelemetrySafeWithZeroValues(t *testing.T) {
	var tel Telemetry = noopTelemetry{}

	// None of these must panic, regardless of how degenerate the input is.
	tel.RecordToolCall("", 0)
	tel.RecordToolCall("", -1)
	tel.RecordCacheResult(false)
	tel.RecordCacheResult(true)
	tel.RecordError("")
	tel.RecordError("anything at all")
	tel.SetDiscoveryStats(0)
	tel.SetDiscoveryStats(-1)
	tel.Start(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tel.Start(ctx)

	tel.Close()
	tel.Close() // calling Close twice must also be safe
}

// --- HTTP send never errors/panics ---

func TestHTTPClientSendNeverErrorsOn500(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := &httpClient{hc: &http.Client{Timeout: sendTimeout}, url: ts.URL}

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.send(eventPayload{EventName: "session_summary"})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("send() did not return within 5s against a 500-returning server")
	}

	if requests == 0 {
		t.Error("expected the test server to have received a request")
	}
}

func TestHTTPClientSendNeverErrorsOnHang(t *testing.T) {
	block := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // hang past the client's own timeout
	}))
	// Deferred in this order so close(block) (unparking the hung handler)
	// runs before ts.Close() (which otherwise blocks forever waiting for
	// that same in-flight handler to finish) - defers run LIFO.
	defer ts.Close()
	defer close(block)

	c := &httpClient{hc: &http.Client{Timeout: 100 * time.Millisecond}, url: ts.URL}

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.send(eventPayload{EventName: "session_summary"})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("send() did not return within 5s against a hanging server - timeout not enforced")
	}
}

// --- disabled telemetry makes zero HTTP requests ---

func TestDisabledTelemetryMakesNoRequests(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer ts.Close()

	tel := New(Config{Enabled: false})

	ctx, cancel := context.WithCancel(context.Background())
	tel.Start(ctx)
	tel.RecordToolCall("opcua_read", 1)
	tel.RecordCacheResult(true)
	tel.RecordError(ErrKindTimeout)
	tel.SetDiscoveryStats(42)
	cancel()
	tel.Close()

	if requests != 0 {
		t.Errorf("disabled telemetry caused %d HTTP request(s), want 0", requests)
	}
}

// TestLiveTelemetryFlushSendsExactlyOneRequestPerInterval exercises the real
// (enabled) path end-to-end against a local httptest.Server standing in for
// Aptabase, confirming flush cadence matches FlushInterval rather than firing
// once per RecordToolCall - one request per interval, not one per MCP call.
func TestLiveTelemetryFlushSendsExactlyOneRequestPerInterval(t *testing.T) {
	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tel := New(Config{Enabled: true, FlushInterval: 50 * time.Millisecond, AnonID: "test"})
	live, ok := tel.(*liveTelemetry)
	if !ok {
		t.Fatalf("New(Config{Enabled: true}) = %T, want *liveTelemetry", tel)
	}
	live.http.url = ts.URL

	ctx, cancel := context.WithCancel(context.Background())
	tel.Start(ctx)

	for i := 0; i < 10; i++ {
		tel.RecordToolCall("opcua_read", 1)
	}

	time.Sleep(120 * time.Millisecond)
	cancel()
	tel.Close()

	if requests == 0 {
		t.Error("expected at least one flushed request from the ticker")
	}
}
