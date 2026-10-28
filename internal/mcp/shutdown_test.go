package mcp

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingTelemetry is a telemetry.Telemetry that records whether its
// flush loop was canceled (which is what triggers the final flush) before
// Close() was called, and how many times Close() ran.
type recordingTelemetry struct {
	mu                   sync.Mutex
	ctx                  context.Context
	closeCalls           int
	canceledAtFirstClose bool
}

func (r *recordingTelemetry) RecordToolCall(string, int) {}
func (r *recordingTelemetry) RecordCacheResult(bool)     {}
func (r *recordingTelemetry) RecordError(string)         {}
func (r *recordingTelemetry) SetDiscoveryStats(int)      {}

func (r *recordingTelemetry) Start(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ctx = ctx
}

func (r *recordingTelemetry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCalls++
	if r.closeCalls == 1 && r.ctx != nil {
		r.canceledAtFirstClose = r.ctx.Err() != nil
	}
}

// newShutdownTestServer wires a recordingTelemetry into a test Server the
// same way Start() does, without Start()'s transport loop.
func newShutdownTestServer(t *testing.T) (*Server, *recordingTelemetry) {
	t.Helper()
	s := newTestServer(t)
	rec := &recordingTelemetry{}
	s.SetTelemetry(rec)
	ctx, cancel := context.WithCancel(context.Background())
	s.telemetryCancel = cancel
	s.telemetry.Start(ctx)
	return s, rec
}

// TestServeStdioFlushesTelemetryOnStdinEOF covers the stdio path where the
// MCP client closes stdin instead of sending a signal: serveStdio must run
// the shutdown sequence, so telemetry's flush loop is canceled (triggering
// its final flush) before Close() waits on it. Previously the process exited
// straight from main(), losing every event from sessions shorter than the
// flush interval.
func TestServeStdioFlushesTelemetryOnStdinEOF(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
	}{
		{name: "empty session", stdin: ""},
		{name: "session with a request", stdin: `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, rec := newShutdownTestServer(t)

			done := make(chan error, 1)
			go func() {
				done <- s.serveStdio(context.Background(), strings.NewReader(tt.stdin), io.Discard)
			}()

			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("serveStdio() error: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("serveStdio() did not return within 5s of stdin EOF")
			}

			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.closeCalls != 1 {
				t.Errorf("telemetry.Close() called %d times, want 1", rec.closeCalls)
			}
			if !rec.canceledAtFirstClose {
				t.Error("telemetry flush loop was not canceled before Close(), so no final flush would be sent")
			}
		})
	}
}

// TestShutdownRunsOnce covers the race between a signal and stdin EOF both
// triggering shutdown: the sequence must run exactly once, or
// DiscoveryService.Stop would close its stop channel twice and panic.
func TestShutdownRunsOnce(t *testing.T) {
	s, rec := newShutdownTestServer(t)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.shutdown()
		}()
	}
	wg.Wait()

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.closeCalls != 1 {
		t.Errorf("telemetry.Close() called %d times, want 1", rec.closeCalls)
	}
}
