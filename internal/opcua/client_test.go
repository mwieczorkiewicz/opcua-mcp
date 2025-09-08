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
