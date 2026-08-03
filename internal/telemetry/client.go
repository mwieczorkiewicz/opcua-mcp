package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
)

// Aptabase hosts ingest per data-residency region, encoded in the App Key's
// prefix (A-US-/A-EU-/A-SH-...) - events must be POSTed to the host matching
// the key's own region, not a fixed one, or they're silently rejected.
//
// The path is /api/v0/events (plural) expecting a JSON array body, even for
// a single event - not /api/v0/event with a bare object, per Aptabase's own
// "build your own SDK" reference
// (https://github.com/aptabase/aptabase/wiki/How-to-build-your-own-SDK).
const (
	usIngestURL = "https://us.aptabase.com/api/v0/events"
	euIngestURL = "https://eu.aptabase.com/api/v0/events"
)

// ingestURLForKey resolves the ingest host from an App Key's region prefix.
// Self-hosted (A-SH-) keys point at an operator-chosen custom host we have
// no way to auto-discover, so - like any other unrecognized prefix - they
// fall back to the US endpoint rather than guessing wrong silently.
func ingestURLForKey(key string) string {
	if strings.HasPrefix(key, "A-EU-") {
		return euIngestURL
	}
	return usIngestURL
}

// sendTimeout bounds how long a single event send may take.
const sendTimeout = 2 * time.Second

// appKey is the Aptabase App Key sent with every event. This placeholder is
// intentionally not a real key - override it at build time with:
//
//	go build -ldflags "-X github.com/mwieczorkiewicz/opcua-mcp/internal/telemetry.appKey=A-XX-XXXXXXXXXX"
//
// A real key must never be hardcoded or committed to this repository.
var appKey = "A-DEV-0000000000"

// httpClient sends event payloads to Aptabase. It's deliberately tiny - a
// hand-rolled ingest call rather than a dependency on Aptabase's SDK, since
// the ingest contract is a single JSON POST (see BrycensRanch/go-aptabase
// for reference, not vendored here).
type httpClient struct {
	hc  *http.Client
	url string
}

func newHTTPClient() *httpClient {
	return &httpClient{
		hc:  &http.Client{Timeout: sendTimeout},
		url: ingestURLForKey(appKey),
	}
}

// send POSTs one event payload. It never returns an error: telemetry must
// never be able to affect server correctness or availability, so any
// failure - marshal error, network error, timeout, non-2xx response - is
// logged at debug level only and otherwise dropped. There is no retry;
// losing an occasional session_summary event is an acceptable tradeoff for
// that guarantee. The panic recovery below is defense in depth for the same
// reason: even an unexpected panic inside this call must never propagate to
// the caller (a background goroutine spawned by (*liveTelemetry).flush).
//
// The body is a JSON array wrapping the single payload, even though we only
// ever send one event per flush - Aptabase's ingest endpoint is batch-only
// and rejects a bare object.
func (c *httpClient) send(payload eventPayload) {
	defer func() {
		if r := recover(); r != nil {
			logger.Debug("telemetry: send panicked, dropping event", "recovered", r)
		}
	}()

	body, err := json.Marshal([]eventPayload{payload})
	if err != nil {
		logger.Debug("telemetry: failed to marshal event, dropping", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		logger.Debug("telemetry: failed to build request, dropping event", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("App-Key", appKey)

	resp, err := c.hc.Do(req)
	if err != nil {
		logger.Debug("telemetry: send failed, dropping event", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		logger.Debug("telemetry: non-2xx response, event may not have been recorded", "status", resp.StatusCode)
		return
	}

	// A positive confirmation, not just the absence of a failure log above -
	// two separate silent-failure bugs (wrong endpoint, missing CA certs)
	// were each hard to diagnose from an all-quiet run with no explicit
	// success signal at any log level to compare against.
	logger.Debug("telemetry: event sent", "status", resp.StatusCode)
}
