package opcua

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
)

func TestNewClient(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.config != cfg {
		t.Error("Client config not set correctly")
	}

	if client.connected {
		t.Error("Client should not be connected initially")
	}
}

func TestClientIsConnected(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)

	// Initially not connected
	if client.IsConnected() {
		t.Error("Client should not be connected initially")
	}

	// Set connected flag manually for testing
	client.SetConnectedForTesting(true)
	// Create a mock client to satisfy the IsConnected check
	client.client = &opcua.Client{}
	if !client.IsConnected() {
		t.Error("Client should be connected after setting flag")
	}
}

func TestClientDisconnectWhenNotConnected(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Disconnect when not connected should not error
	err := client.Disconnect(ctx)
	if err != nil {
		t.Errorf("Disconnect when not connected should not error, got: %v", err)
	}
}

func TestClientReadWhenNotConnected(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Read when not connected should error
	_, err := client.Read(ctx, []string{"ns=2;i=1"})
	if err == nil {
		t.Error("Read when not connected should error")
	}

	expectedErr := "client is not connected"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestClientWriteWhenNotConnected(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Write when not connected should error
	err := client.Write(ctx, "ns=2;i=1", "test")
	if err == nil {
		t.Error("Write when not connected should error")
	}

	expectedErr := "client is not connected"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestClientBrowseWhenNotConnected(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// Browse when not connected should error
	_, err := client.Browse(ctx, "i=85")
	if err == nil {
		t.Error("Browse when not connected should error")
	}

	expectedErr := "client is not connected"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestClientGetNodeClassWhenNotConnected(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// GetNodeClass when not connected should error
	_, err := client.GetNodeClass(ctx, "ns=2;i=1")
	if err == nil {
		t.Error("GetNodeClass when not connected should error")
	}

	expectedErr := "client is not connected"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestClientGetServerInfoWhenNotConnected(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	// GetServerInfo when not connected should error
	_, err := client.GetServerInfo(ctx)
	if err == nil {
		t.Error("GetServerInfo when not connected should error")
	}

	expectedErr := "client is not connected"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

// TestClientConcurrentAccess drives IsConnected()/Read() from one goroutine while
// SetConnectedForTesting() toggles state from another, to exercise P0-1's mutex
// under `go test -race`. c.client is never assigned a live connection here, so
// Read() always short-circuits on the "not connected" check before touching the
// zero-value *opcua.Client — this validates the mu-guarded field access itself,
// not real RPC behavior.
func TestClientConcurrentAccess(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	ctx := context.Background()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				client.IsConnected()
				_, _ = client.Read(ctx, []string{"i=85"})
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			client.SetConnectedForTesting(i%2 == 0)
		}
		close(stop)
	}()

	wg.Wait()
}

// TestDecodeNodeClass covers P0-3 (findings.md H10a): NodeClass decodes on the
// wire as a plain int32, not ua.NodeClass, so decodeNodeClass must assert to
// int32 and reject non-positive values rather than asserting to ua.NodeClass
// directly (which always fails against a real server).
func TestDecodeNodeClass(t *testing.T) {
	tests := []struct {
		name      string
		raw       interface{}
		want      ua.NodeClass
		wantError bool
	}{
		{name: "object", raw: int32(1), want: ua.NodeClassObject},
		{name: "variable", raw: int32(2), want: ua.NodeClassVariable},
		{name: "method", raw: int32(4), want: ua.NodeClassMethod},
		{name: "view", raw: int32(128), want: ua.NodeClassView},
		{name: "zero is invalid", raw: int32(0), wantError: true},
		{name: "negative is invalid", raw: int32(-1), wantError: true},
		{name: "wrong wire type ua.NodeClass", raw: ua.NodeClassObject, wantError: true},
		{name: "wrong type string", raw: "1", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeNodeClass(tt.raw)
			if tt.wantError {
				if err == nil {
					t.Fatalf("decodeNodeClass(%v) expected error, got nil", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeNodeClass(%v) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("decodeNodeClass(%v) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// TestParseStringToInt64/UInt64/Float64 cover P0-4 (findings.md H10b): the old
// fmt.Sscanf-based parsing didn't require the whole string to be consumed, so
// "12abc" silently parsed as 12 instead of erroring - a malformed numeric
// input from an MCP tool caller could write a wrong value to a live device.
func TestParseStringToInt64(t *testing.T) {
	c := &Client{}
	tests := []struct {
		input     string
		want      int64
		wantError bool
	}{
		{input: "42", want: 42},
		{input: " 42 ", want: 42},
		{input: "-7", want: -7},
		{input: "12abc", wantError: true},
		{input: "  ", wantError: true},
		{input: "", wantError: true},
		{input: "12.5", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := c.parseStringToInt64(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatalf("parseStringToInt64(%q) expected error, got nil (value %d)", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStringToInt64(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseStringToInt64(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseStringToUInt64(t *testing.T) {
	c := &Client{}
	tests := []struct {
		input     string
		want      uint64
		wantError bool
	}{
		{input: "42", want: 42},
		{input: " 42 ", want: 42},
		{input: "12abc", wantError: true},
		{input: "  ", wantError: true},
		{input: "", wantError: true},
		{input: "12.5", wantError: true},
		{input: "-7", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := c.parseStringToUInt64(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatalf("parseStringToUInt64(%q) expected error, got nil (value %d)", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStringToUInt64(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseStringToUInt64(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseStringToFloat64(t *testing.T) {
	c := &Client{}
	tests := []struct {
		input     string
		want      float64
		wantError bool
	}{
		{input: "42", want: 42},
		{input: " 42 ", want: 42},
		{input: "-7", want: -7},
		{input: "3.14", want: 3.14},
		{input: "12abc", wantError: true},
		{input: "  ", wantError: true},
		{input: "", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := c.parseStringToFloat64(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatalf("parseStringToFloat64(%q) expected error, got nil (value %g)", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStringToFloat64(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseStringToFloat64(%q) = %g, want %g", tt.input, got, tt.want)
			}
		})
	}
}

// TestWritePropagatesValidationFailure covers P0-6 (findings.md, "Write()
// silently ignores validation failures", high). Uses the opcuaClient mock
// seam to drive Write() through a real GetNodeTypeInfo success path (an
// Int32-typed, writable node) with a value that fails ValidateValueForNode,
// and asserts Write() returns the wrapped error without ever attempting the
// underlying Write RPC.
func TestWritePropagatesValidationFailure(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	mock := &mockOpcuaClient{
		readFunc: typeInfoReadFunc(ua.NewNumericNodeID(0, uint32(ua.TypeIDInt32)), writeAccessBit),
	}
	client.client = mock
	client.connected = true

	ctx := context.Background()
	err := client.Write(ctx, "ns=2;i=1", "not-an-int")

	if err == nil {
		t.Fatal("Write() with a type-mismatched value expected an error, got nil")
	}
	if mock.writeCalls != 0 {
		t.Errorf("Write() issued %d Write RPC(s) after validation failed, want 0", mock.writeCalls)
	}
}

// TestReadReturnsPerNodePartialResults covers P1-1 (findings.md H3, high):
// one bad-status node in a batch must not discard the good results for
// every other node in the same batch, and no top-level error should be
// returned for per-node status failures (only transport-level failures do).
func TestReadReturnsPerNodePartialResults(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	mock := &mockOpcuaClient{
		readFunc: func(ctx context.Context, req *ua.ReadRequest) (*ua.ReadResponse, error) {
			return &ua.ReadResponse{
				Results: []*ua.DataValue{
					{Status: ua.StatusOK, Value: ua.MustVariant(int32(42))},
					{Status: ua.StatusBadNodeIDUnknown, Value: ua.MustVariant(int32(0))},
					{Status: ua.StatusOK, Value: ua.MustVariant(int32(7))},
				},
			}, nil
		},
	}
	client.client = mock
	client.connected = true

	results, err := client.Read(context.Background(), []string{"i=1", "i=2", "i=3"})
	if err != nil {
		t.Fatalf("Read() unexpected top-level error for a mixed-status batch: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Read() returned %d results, want 3 (one per requested node)", len(results))
	}

	if results[0].Status != ua.StatusOK || results[0].Value.Value() != int32(42) {
		t.Errorf("results[0] = %+v, want status OK / value 42", results[0])
	}
	if results[1].Status == ua.StatusOK {
		t.Errorf("results[1] expected a bad status, got OK")
	}
	if results[2].Status != ua.StatusOK || results[2].Value.Value() != int32(7) {
		t.Errorf("results[2] = %+v, want status OK / value 7", results[2])
	}
}

// TestWriteFetchesNodeTypeInfoOnce covers P1-4 (findings.md H4, partial
// fix): Write() used to call GetNodeTypeInfo directly and then again inside
// ValidateValueForNode, doubling the 5 underlying attribute reads to 10.
// ValidateValueForNode now takes the already-fetched *NodeTypeInfo, so one
// Write() call should issue exactly 5 reads (the full fix - a persistent
// typeinfo cache across separate Write() calls - lands with P2-8).
func TestWriteFetchesNodeTypeInfoOnce(t *testing.T) {
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}

	client := NewClient(cfg)
	mock := &mockOpcuaClient{
		readFunc: typeInfoReadFunc(ua.NewNumericNodeID(0, uint32(ua.TypeIDInt32)), writeAccessBit),
	}
	client.client = mock
	client.connected = true

	if err := client.Write(context.Background(), "ns=2;i=1", int32(42)); err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}

	if mock.readCalls != 5 {
		t.Errorf("Write() issued %d Read calls, want exactly 5 (not the pre-P1-4 10)", mock.readCalls)
	}
	if mock.writeCalls != 1 {
		t.Errorf("Write() issued %d Write RPC(s), want 1", mock.writeCalls)
	}
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	cfg := &config.OPCUAConfig{
		Endpoint:       "opc.tcp://localhost:4840",
		AuthMode:       "anonymous",
		SecurityPolicy: "None",
		SecurityMode:   "None",
		RequestTimeout: 30 * time.Second,
		SessionTimeout: 60 * time.Second,
		MaxRetries:     3,
		RetryDelay:     1 * time.Second,
	}
	return NewClient(cfg)
}

// TestBrowseNoContinuation covers the no-regression case for P1-2: a
// well-behaved server that returns everything in one page makes exactly one
// Browse call and zero BrowseNext calls.
func TestBrowseNoContinuation(t *testing.T) {
	client := newTestClient(t)
	mock := &mockOpcuaClient{
		browseFunc: func(ctx context.Context, req *ua.BrowseRequest) (*ua.BrowseResponse, error) {
			return &ua.BrowseResponse{
				Results: []*ua.BrowseResult{
					{
						StatusCode: ua.StatusOK,
						References: []*ua.ReferenceDescription{
							{BrowseName: &ua.QualifiedName{Name: "child1"}},
							{BrowseName: &ua.QualifiedName{Name: "child2"}},
						},
					},
				},
			}, nil
		},
	}
	client.client = mock
	client.connected = true

	refs, err := client.Browse(context.Background(), "i=85")
	if err != nil {
		t.Fatalf("Browse() unexpected error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("Browse() returned %d references, want 2", len(refs))
	}
	if mock.browseCalls != 1 {
		t.Errorf("Browse() issued %d Browse call(s), want 1", mock.browseCalls)
	}
	if mock.browseNextCalls != 0 {
		t.Errorf("Browse() issued %d BrowseNext call(s), want 0 for a single-page result", mock.browseNextCalls)
	}
}

// TestBrowseFollowsContinuationPoint covers P1-2 (findings.md H5, medium):
// Browse must follow BrowseNext continuation points instead of silently
// truncating results past RequestedMaxReferencesPerNode.
func TestBrowseFollowsContinuationPoint(t *testing.T) {
	client := newTestClient(t)
	mock := &mockOpcuaClient{
		browseFunc: func(ctx context.Context, req *ua.BrowseRequest) (*ua.BrowseResponse, error) {
			return &ua.BrowseResponse{
				Results: []*ua.BrowseResult{
					{
						StatusCode:        ua.StatusOK,
						ContinuationPoint: []byte("page-2"),
						References: []*ua.ReferenceDescription{
							{BrowseName: &ua.QualifiedName{Name: "child1"}},
						},
					},
				},
			}, nil
		},
		browseNextFunc: func(ctx context.Context, req *ua.BrowseNextRequest) (*ua.BrowseNextResponse, error) {
			if len(req.ContinuationPoints) != 1 || string(req.ContinuationPoints[0]) != "page-2" {
				t.Fatalf("BrowseNext() got continuation points %v, want [\"page-2\"]", req.ContinuationPoints)
			}
			return &ua.BrowseNextResponse{
				Results: []*ua.BrowseResult{
					{
						StatusCode: ua.StatusOK,
						// No continuation point: this is the last page.
						References: []*ua.ReferenceDescription{
							{BrowseName: &ua.QualifiedName{Name: "child2"}},
						},
					},
				},
			}, nil
		},
	}
	client.client = mock
	client.connected = true

	refs, err := client.Browse(context.Background(), "i=85")
	if err != nil {
		t.Fatalf("Browse() unexpected error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("Browse() returned %d references across 2 pages, want 2", len(refs))
	}
	if refs[0].BrowseName.Name != "child1" || refs[1].BrowseName.Name != "child2" {
		t.Errorf("Browse() references = [%q, %q], want [child1, child2]", refs[0].BrowseName.Name, refs[1].BrowseName.Name)
	}
	if mock.browseCalls != 1 || mock.browseNextCalls != 1 {
		t.Errorf("Browse() issued %d Browse and %d BrowseNext calls, want 1 and 1", mock.browseCalls, mock.browseNextCalls)
	}
}

// TestBrowseContinuationLoopIsBounded covers P1-2's iteration-cap acceptance
// criterion: a server that never exhausts its continuation point must not
// hang Browse() forever.
func TestBrowseContinuationLoopIsBounded(t *testing.T) {
	client := newTestClient(t)
	mock := &mockOpcuaClient{
		browseFunc: func(ctx context.Context, req *ua.BrowseRequest) (*ua.BrowseResponse, error) {
			return &ua.BrowseResponse{
				Results: []*ua.BrowseResult{{StatusCode: ua.StatusOK, ContinuationPoint: []byte("more")}},
			}, nil
		},
		browseNextFunc: func(ctx context.Context, req *ua.BrowseNextRequest) (*ua.BrowseNextResponse, error) {
			// Always claims there's another page - a misbehaving/malicious server.
			return &ua.BrowseNextResponse{
				Results: []*ua.BrowseResult{{StatusCode: ua.StatusOK, ContinuationPoint: []byte("more")}},
			}, nil
		},
	}
	client.client = mock
	client.connected = true

	_, err := client.Browse(context.Background(), "i=85")
	if err == nil {
		t.Fatal("Browse() against a server that never exhausts its continuation point expected an error, got nil")
	}
	if mock.browseNextCalls != maxBrowseNextIterations {
		t.Errorf("Browse() issued %d BrowseNext calls, want exactly the %d-iteration cap", mock.browseNextCalls, maxBrowseNextIterations)
	}
}

func TestParseNodeIDs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		hasError bool
	}{
		{
			name:     "single numeric node ID",
			input:    "i=85",
			expected: []string{"i=85"},
			hasError: false,
		},
		{
			name:     "single string node ID",
			input:    "s=Temperature",
			expected: []string{"s=Temperature"},
			hasError: false,
		},
		{
			name:     "single GUID node ID",
			input:    "g=12345678-1234-1234-1234-123456789abc",
			expected: []string{"g=12345678-1234-1234-1234-123456789abc"},
			hasError: false,
		},
		{
			name:     "comma-separated node IDs",
			input:    "i=85,i=86,i=87",
			expected: []string{"i=85", "i=86", "i=87"},
			hasError: false,
		},
		{
			name:     "comma-separated with spaces",
			input:    "i=85, i=86, i=87",
			expected: []string{"i=85", "i=86", "i=87"},
			hasError: false,
		},
		{
			name:     "JSON array format",
			input:    `["i=85","i=86","i=87"]`,
			expected: []string{"i=85", "i=86", "i=87"},
			hasError: false,
		},
		{
			name:     "JSON array with spaces",
			input:    `[ "i=85" , "i=86" , "i=87" ]`,
			expected: []string{"i=85", "i=86", "i=87"},
			hasError: false,
		},
		{
			name:     "quoted single node ID",
			input:    `"i=85"`,
			expected: []string{"i=85"},
			hasError: false,
		},
		{
			name:     "quoted comma-separated node IDs",
			input:    `"i=85","i=86","i=87"`,
			expected: []string{"i=85", "i=86", "i=87"},
			hasError: false,
		},
		{
			name:     "mixed node ID types",
			input:    "i=85,s=Temperature,g=12345678-1234-1234-1234-123456789abc",
			expected: []string{"i=85", "s=Temperature", "g=12345678-1234-1234-1234-123456789abc"},
			hasError: false,
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
			hasError: true,
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: nil,
			hasError: true,
		},
		{
			name:     "string node ID without prefix",
			input:    "invalid",
			expected: []string{"invalid"},
			hasError: false,
		},
		{
			name:     "comma-separated with empty element",
			input:    "i=85,,i=87",
			expected: nil,
			hasError: true,
		},
		{
			name:     "comma-separated with string elements",
			input:    "i=85,invalid,i=87",
			expected: []string{"i=85", "invalid", "i=87"},
			hasError: false,
		},
		{
			name:     "JSON array with empty element",
			input:    `["i=85","","i=87"]`,
			expected: nil,
			hasError: true,
		},
		{
			name:     "JSON array with string elements",
			input:    `["i=85","invalid","i=87"]`,
			expected: []string{"i=85", "invalid", "i=87"},
			hasError: false,
		},
		{
			name:     "malformed JSON",
			input:    `["i=85","i=86"`,
			expected: nil,
			hasError: true,
		},
		{
			name:     "node ID with namespace",
			input:    "ns=2;i=85",
			expected: []string{"ns=2;i=85"},
			hasError: false,
		},
		{
			name:     "multiple namespaced node IDs",
			input:    "ns=2;i=85,ns=3;s=Temperature",
			expected: []string{"ns=2;i=85", "ns=3;s=Temperature"},
			hasError: false,
		},
		{
			name:     "space-separated invalid single node ID",
			input:    "i=85 i=86 i=87",
			expected: nil,
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseNodeIDs(tt.input)

			if tt.hasError {
				if err == nil {
					t.Errorf("ParseNodeIDs(%q) expected error, got none", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseNodeIDs(%q) unexpected error: %v", tt.input, err)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("ParseNodeIDs(%q) expected %d results, got %d", tt.input, len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("ParseNodeIDs(%q) result[%d] = %q, expected %q", tt.input, i, result[i], expected)
				}
			}
		})
	}
}
