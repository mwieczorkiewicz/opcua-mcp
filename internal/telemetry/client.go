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
const (
	usIngestURL = "https://us.aptabase.com/api/v0/event"
	euIngestURL = "https://eu.aptabase.com/api/v0/event"
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
func (c *httpClient) send(payload eventPayload) {
	defer func() {
		if r := recover(); r != nil {
			logger.Debug("telemetry: send panicked, dropping event", "recovered", r)
		}
	}()

	body, err := json.Marshal(payload)
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
	}
}
