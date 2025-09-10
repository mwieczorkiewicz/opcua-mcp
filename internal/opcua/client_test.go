package opcua

import (
	"context"
	"testing"
	"time"

	"github.com/gopcua/opcua"
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
