package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Set test environment variables
	os.Setenv("SERVER_TRANSPORT", "http")
	os.Setenv("SERVER_HTTP_PORT", "9090")
	os.Setenv("OPCUA_ENDPOINT", "opc.tcp://test:4840")
	os.Setenv("OPCUA_AUTH_MODE", "username")
	os.Setenv("OPCUA_USERNAME", "testuser")
	os.Setenv("OPCUA_PASSWORD", "testpass")
	os.Setenv("MCP_NAME", "Test Server")

	// Load configuration
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	// Verify configuration
	if cfg.Server.Transport != "http" {
		t.Errorf("Expected transport 'http', got '%s'", cfg.Server.Transport)
	}
	if cfg.Server.HTTPPort != "9090" {
		t.Errorf("Expected HTTP port '9090', got '%s'", cfg.Server.HTTPPort)
	}
	if cfg.OPCUA.Endpoint != "opc.tcp://test:4840" {
		t.Errorf("Expected endpoint 'opc.tcp://test:4840', got '%s'", cfg.OPCUA.Endpoint)
	}
	if cfg.OPCUA.AuthMode != "username" {
		t.Errorf("Expected auth mode 'username', got '%s'", cfg.OPCUA.AuthMode)
	}
	if cfg.OPCUA.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", cfg.OPCUA.Username)
	}
	if cfg.OPCUA.Password != "testpass" {
		t.Errorf("Expected password 'testpass', got '%s'", cfg.OPCUA.Password)
	}
	if cfg.MCP.Name != "Test Server" {
		t.Errorf("Expected MCP name 'Test Server', got '%s'", cfg.MCP.Name)
	}

	// Clean up
	os.Unsetenv("SERVER_TRANSPORT")
	os.Unsetenv("SERVER_HTTP_PORT")
	os.Unsetenv("OPCUA_ENDPOINT")
	os.Unsetenv("OPCUA_AUTH_MODE")
	os.Unsetenv("OPCUA_USERNAME")
	os.Unsetenv("OPCUA_PASSWORD")
	os.Unsetenv("MCP_NAME")
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid stdio config",
			config: &Config{
				Server: ServerConfig{
					Transport: "stdio",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "anonymous",
					SecurityPolicy: "None",
					SecurityMode:   "None",
				},
			},
			wantErr: false,
		},
		{
			name: "valid http config",
			config: &Config{
				Server: ServerConfig{
					Transport: "http",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "anonymous",
					SecurityPolicy: "None",
					SecurityMode:   "None",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid transport",
			config: &Config{
				Server: ServerConfig{
					Transport: "invalid",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "anonymous",
					SecurityPolicy: "None",
					SecurityMode:   "None",
				},
			},
			wantErr: true,
		},
		{
			name: "valid username auth",
			config: &Config{
				Server: ServerConfig{
					Transport: "stdio",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "username",
					Username:       "testuser",
					Password:       "testpass",
					SecurityPolicy: "None",
					SecurityMode:   "None",
				},
			},
			wantErr: false,
		},
		{
			name: "username auth without username",
			config: &Config{
				Server: ServerConfig{
					Transport: "stdio",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "username",
					Password:       "testpass",
					SecurityPolicy: "None",
					SecurityMode:   "None",
				},
			},
			wantErr: true,
		},
		{
			name: "username auth without password",
			config: &Config{
				Server: ServerConfig{
					Transport: "stdio",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "username",
					Username:       "testuser",
					SecurityPolicy: "None",
					SecurityMode:   "None",
				},
			},
			wantErr: true,
		},
		{
			name: "certificate auth without cert file",
			config: &Config{
				Server: ServerConfig{
					Transport: "stdio",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "certificate",
					KeyFile:        "key.pem",
					SecurityPolicy: "None",
					SecurityMode:   "None",
				},
			},
			wantErr: true,
		},
		{
			name: "certificate auth without key file",
			config: &Config{
				Server: ServerConfig{
					Transport: "stdio",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "certificate",
					CertFile:       "cert.pem",
					SecurityPolicy: "None",
					SecurityMode:   "None",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid auth mode",
			config: &Config{
				Server: ServerConfig{
					Transport: "stdio",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "invalid",
					SecurityPolicy: "None",
					SecurityMode:   "None",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid security policy",
			config: &Config{
				Server: ServerConfig{
					Transport: "stdio",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "anonymous",
					SecurityPolicy: "Invalid",
					SecurityMode:   "None",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid security mode",
			config: &Config{
				Server: ServerConfig{
					Transport: "stdio",
				},
				OPCUA: OPCUAConfig{
					AuthMode:       "anonymous",
					SecurityPolicy: "None",
					SecurityMode:   "Invalid",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{
		Field:   "TEST_FIELD",
		Message: "test message",
	}

	expected := "TEST_FIELD: test message"
	if err.Error() != expected {
		t.Errorf("ValidationError.Error() = %v, want %v", err.Error(), expected)
	}
}

func TestDefaultValues(t *testing.T) {
	// Clear environment variables
	envVars := []string{
		"SERVER_TRANSPORT", "SERVER_HTTP_PORT", "SERVER_LOG_LEVEL",
		"OPCUA_ENDPOINT", "OPCUA_AUTH_MODE", "OPCUA_SECURITY_POLICY", "OPCUA_SECURITY_MODE",
		"OPCUA_REQUEST_TIMEOUT", "OPCUA_SESSION_TIMEOUT", "OPCUA_MAX_RETRIES", "OPCUA_RETRY_DELAY",
		"MCP_NAME", "MCP_VERSION", "MCP_ENABLE_TOOLS", "MCP_ENABLE_RESOURCES", "MCP_ENABLE_PROMPTS", "MCP_HTTP_PATH",
	}

	for _, envVar := range envVars {
		os.Unsetenv(envVar)
	}

	// Load configuration with defaults
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	// Check default values
	if cfg.Server.Transport != "stdio" {
		t.Errorf("Expected default transport 'stdio', got '%s'", cfg.Server.Transport)
	}
	if cfg.Server.HTTPPort != "8080" {
		t.Errorf("Expected default HTTP port '8080', got '%s'", cfg.Server.HTTPPort)
	}
	if cfg.Server.LogLevel != "info" {
		t.Errorf("Expected default log level 'info', got '%s'", cfg.Server.LogLevel)
	}
	if cfg.OPCUA.Endpoint != "opc.tcp://localhost:4840" {
		t.Errorf("Expected default endpoint 'opc.tcp://localhost:4840', got '%s'", cfg.OPCUA.Endpoint)
	}
	if cfg.OPCUA.AuthMode != "anonymous" {
		t.Errorf("Expected default auth mode 'anonymous', got '%s'", cfg.OPCUA.AuthMode)
	}
	if cfg.OPCUA.SecurityPolicy != "None" {
		t.Errorf("Expected default security policy 'None', got '%s'", cfg.OPCUA.SecurityPolicy)
	}
	if cfg.OPCUA.SecurityMode != "None" {
		t.Errorf("Expected default security mode 'None', got '%s'", cfg.OPCUA.SecurityMode)
	}
	if cfg.OPCUA.RequestTimeout != 30*time.Second {
		t.Errorf("Expected default request timeout 30s, got %v", cfg.OPCUA.RequestTimeout)
	}
	if cfg.OPCUA.SessionTimeout != 60*time.Second {
		t.Errorf("Expected default session timeout 60s, got %v", cfg.OPCUA.SessionTimeout)
	}
	if cfg.OPCUA.MaxRetries != 3 {
		t.Errorf("Expected default max retries 3, got %d", cfg.OPCUA.MaxRetries)
	}
	if cfg.OPCUA.RetryDelay != 1*time.Second {
		t.Errorf("Expected default retry delay 1s, got %v", cfg.OPCUA.RetryDelay)
	}
	if cfg.MCP.Name != "OPC-UA MCP Server" {
		t.Errorf("Expected default MCP name 'OPC-UA MCP Server', got '%s'", cfg.MCP.Name)
	}
	if cfg.MCP.Version != "1.0.0" {
		t.Errorf("Expected default MCP version '1.0.0', got '%s'", cfg.MCP.Version)
	}
	if !cfg.MCP.EnableTools {
		t.Errorf("Expected default enable tools true, got %v", cfg.MCP.EnableTools)
	}
	if !cfg.MCP.EnableResources {
		t.Errorf("Expected default enable resources true, got %v", cfg.MCP.EnableResources)
	}
	if cfg.MCP.EnablePrompts {
		t.Errorf("Expected default enable prompts false, got %v", cfg.MCP.EnablePrompts)
	}
	if cfg.MCP.HTTPPath != "/mcp" {
		t.Errorf("Expected default HTTP path '/mcp', got '%s'", cfg.MCP.HTTPPath)
	}
	if cfg.Store.DBPath != "mcp_opcua_store.db" {
		t.Errorf("Expected default store DB path 'mcp_opcua_store.db', got '%s'", cfg.Store.DBPath)
	}
	if cfg.Store.OpenTimeout != 5*time.Second {
		t.Errorf("Expected default store open timeout 5s, got %v", cfg.Store.OpenTimeout)
	}
	if cfg.Store.TypeInfoTTL != 24*time.Hour {
		t.Errorf("Expected default store typeinfo TTL 24h, got %v", cfg.Store.TypeInfoTTL)
	}
	if cfg.Store.BrowseTTL != 5*time.Minute {
		t.Errorf("Expected default store browse TTL 5m, got %v", cfg.Store.BrowseTTL)
	}
	if cfg.Store.BatchWindow != 25*time.Millisecond {
		t.Errorf("Expected default store batch window 25ms, got %v", cfg.Store.BatchWindow)
	}
	if cfg.Store.BatchMaxItems != 250 {
		t.Errorf("Expected default store batch max items 250, got %d", cfg.Store.BatchMaxItems)
	}
	if cfg.Store.NotifyChanBuffer != 1024 {
		t.Errorf("Expected default store notify chan buffer 1024, got %d", cfg.Store.NotifyChanBuffer)
	}
}
