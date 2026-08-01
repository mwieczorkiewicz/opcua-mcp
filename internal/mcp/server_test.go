package mcp

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/opcua"
)

// newTestConfig builds a minimal config for exercising Server without a real
// OPC-UA backend or on-disk search index (EnableDiscovery/EnableSearch off).
func newTestConfig(httpPort string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			HTTPPort:  httpPort,
			Transport: "http",
			LogLevel:  "error",
			LogFormat: "json",
			LogOutput: "stderr",
		},
		OPCUA: config.OPCUAConfig{
			Endpoint:       "opc.tcp://localhost:4840",
			AuthMode:       "anonymous",
			SecurityPolicy: "None",
			SecurityMode:   "None",
			RequestTimeout: 30 * time.Second,
			SessionTimeout: 60 * time.Second,
			MaxRetries:     1,
			RetryDelay:     time.Millisecond,
		},
		MCP: config.MCPConfig{
			Name:    "test",
			Version: "0.0.0",
		},
		Search: config.SearchConfig{
			EnableDiscovery: false,
			EnableSearch:    false,
		},
	}
}

// waitForListener polls addr until a TCP connection succeeds or the timeout
// elapses, to avoid an arbitrary fixed sleep before hitting the server.
func waitForListener(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nothing listening on %s after %s", addr, timeout)
}

// TestHTTPServerShutdownClosesListener covers P1-6 (findings.md H10c,
// medium): setupGracefulShutdown previously kept no reference to the
// running HTTP server, so os.Exit(0) tore down the listening socket
// immediately without draining in-flight requests. startHTTP now stores the
// *server.StreamableHTTPServer handle on Server; this test drives that
// handle directly (this test file is in package mcp, so it can reach the
// unexported field) to confirm Shutdown() actually closes the listener
// rather than relying on OS signal delivery, which is impractical to drive
// reliably from a unit test.
func TestHTTPServerShutdownClosesListener(t *testing.T) {
	const addr = "127.0.0.1:18093"
	cfg := newTestConfig("18093")

	opcuaClient := opcua.NewClient(&cfg.OPCUA)
	s, err := NewServer(cfg, opcuaClient)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	if err := s.SetupTools(); err != nil {
		t.Fatalf("SetupTools() error: %v", err)
	}
	if err := s.SetupResources(); err != nil {
		t.Fatalf("SetupResources() error: %v", err)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- s.startHTTP()
	}()

	waitForListener(t, addr, 2*time.Second)

	// startHTTP must have published the handle before Start() blocks on
	// ListenAndServe, since the listener is already accepting connections.
	s.httpServerMu.Lock()
	httpServer := s.httpServer
	s.httpServerMu.Unlock()
	if httpServer == nil {
		t.Fatal("startHTTP() did not store s.httpServer before serving")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		t.Errorf("httpServer.Shutdown() error: %v", err)
	}

	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("startHTTP() returned %v, want nil or http.ErrServerClosed after Shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startHTTP() did not return within 2s of Shutdown() completing")
	}

	if _, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		t.Error("listener still accepting connections after Shutdown()")
	}
}
