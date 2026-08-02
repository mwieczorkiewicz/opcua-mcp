package mcp

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
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

// newTestServer builds a Server against a Client that is constructed but
// never connected - every handler test below relies on this to reach a
// handler's argument-validation branch (or its "not connected" I/O-error
// branch) without ever attempting a real network dial: IsConnected() and the
// Read/Write/Browse "not connected" checks are all pure in-process checks
// against Client's unconnected zero state.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := newTestConfig("0")
	opcuaClient := opcua.NewClient(&cfg.OPCUA)
	s, err := NewServer(cfg, opcuaClient)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	return s
}

func callTool(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
}

// TestHandlerRequiredArgumentValidation covers P1-7's baseline: every tool
// handler with a required string argument must reject a call missing it via
// IsError, not a Go error return (mcp-go tool handlers report failures
// in-band).
func TestHandlerRequiredArgumentValidation(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{"handleRead missing node_ids", s.handleRead, map[string]any{}},
		{"handleWrite missing node_id", s.handleWrite, map[string]any{"value": "1"}},
		{"handleWrite missing value", s.handleWrite, map[string]any{"node_id": "i=1"}},
		{"handleWrite malformed JSON value", s.handleWrite, map[string]any{"node_id": "i=1", "value": "{not json"}},
		{"handleNodeInfo missing node_id", s.handleNodeInfo, map[string]any{}},
		{"handleGetValue missing node_id", s.handleGetValue, map[string]any{}},
		{"handleGetValueByName missing browse_name", s.handleGetValueByName, map[string]any{}},
		{"handleFindSimilarNodes missing browse_name", s.handleFindSimilarNodes, map[string]any{}},
		{"handleDebugSearch missing browse_name", s.handleDebugSearch, map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.handler(ctx, callTool(tt.args))
			if err != nil {
				t.Fatalf("handler returned unexpected Go error: %v", err)
			}
			if result == nil || !result.IsError {
				t.Errorf("expected IsError=true for %s, got %+v", tt.name, result)
			}
		})
	}
}

// TestHandlerNotConnectedErrorBranches covers each handler's error path when
// the OPC-UA client isn't connected - a pure in-process check, not a network
// call, so this is safe to run without a live or simulated server.
func TestHandlerNotConnectedErrorBranches(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{"handleRead", s.handleRead, map[string]any{"node_ids": "i=85"}},
		{"handleWrite", s.handleWrite, map[string]any{"node_id": "i=85", "value": "1"}},
		{"handleBrowse", s.handleBrowse, map[string]any{}},
		{"handleNodeInfo", s.handleNodeInfo, map[string]any{"node_id": "i=85"}},
		{"handleServerInfo", s.handleServerInfo, map[string]any{}},
		{"handleGetValue", s.handleGetValue, map[string]any{"node_id": "i=85"}},
		{"handleBrowseNodes", s.handleBrowseNodes, map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.handler(ctx, callTool(tt.args))
			if err != nil {
				t.Fatalf("handler returned unexpected Go error: %v", err)
			}
			if result == nil || !result.IsError {
				t.Errorf("expected IsError=true against a disconnected client for %s, got %+v", tt.name, result)
			}
		})
	}
}

// TestHandleDisconnectWhenNotConnected covers handleDisconnect's non-error
// early-return branch.
func TestHandleDisconnectWhenNotConnected(t *testing.T) {
	s := newTestServer(t)
	result, err := s.handleDisconnect(context.Background(), callTool(nil))
	if err != nil {
		t.Fatalf("handleDisconnect() unexpected Go error: %v", err)
	}
	if result == nil || result.IsError {
		t.Errorf("expected a non-error result when already disconnected, got %+v", result)
	}
}

// TestHandleForceDiscoveryWhenDisabled covers handleForceDiscovery's error
// branch when EnableDiscovery is false (newTestConfig's default).
func TestHandleForceDiscoveryWhenDisabled(t *testing.T) {
	s := newTestServer(t)
	result, err := s.handleForceDiscovery(context.Background(), callTool(nil))
	if err != nil {
		t.Fatalf("handleForceDiscovery() unexpected Go error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Errorf("expected IsError=true when discovery is disabled, got %+v", result)
	}
}
