package config

import (
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all configuration for the OPC-UA MCP Server
type Config struct {
	// Server configuration
	Server ServerConfig `envPrefix:"SERVER_"`

	// OPC-UA configuration
	OPCUA OPCUAConfig `envPrefix:"OPCUA_"`

	// MCP configuration
	MCP MCPConfig `envPrefix:"MCP_"`

	// Search and discovery configuration
	Search SearchConfig `envPrefix:"SEARCH_"`

	// Persistent store configuration (values/typeinfo/browse/subscriptions cache)
	Store StoreConfig `envPrefix:"STORE_"`
}

// ServerConfig holds server-specific configuration
type ServerConfig struct {
	// HTTP server configuration
	HTTPPort string `env:"HTTP_PORT" envDefault:"8080"`

	// Transport mode: "stdio" or "http"
	Transport string `env:"TRANSPORT" envDefault:"stdio"`

	// Logging configuration
	LogLevel     string `env:"LOG_LEVEL" envDefault:"info"`       // debug, info, warn, error
	LogFormat    string `env:"LOG_FORMAT" envDefault:"json"`      // json, text
	LogOutput    string `env:"LOG_OUTPUT" envDefault:"stdout"`    // stdout, stderr, file
	LogFile      string `env:"LOG_FILE" envDefault:""`            // file path if LOG_OUTPUT=file
	LogAddSource bool   `env:"LOG_ADD_SOURCE" envDefault:"false"` // add source file/line info
}

// OPCUAConfig holds OPC-UA client configuration
type OPCUAConfig struct {
	// Connection settings
	Endpoint string `env:"ENDPOINT" envDefault:"opc.tcp://localhost:4840"`

	// Authentication settings
	AuthMode   string `env:"AUTH_MODE" envDefault:"anonymous"` // anonymous, username, certificate
	Username   string `env:"USERNAME"`
	Password   string `env:"PASSWORD"`
	CertFile   string `env:"CERT_FILE"`
	KeyFile    string `env:"KEY_FILE"`
	ServerCert string `env:"SERVER_CERT"`

	// Security settings
	SecurityPolicy string `env:"SECURITY_POLICY" envDefault:"None"` // None, Basic128Rsa15, Basic256, Basic256Sha256, Aes128_Sha256_RsaOaep
	SecurityMode   string `env:"SECURITY_MODE" envDefault:"None"`   // None, Sign, SignAndEncrypt

	// Connection settings
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" envDefault:"30s"`
	SessionTimeout time.Duration `env:"SESSION_TIMEOUT" envDefault:"60s"`

	// Retry settings
	MaxRetries int           `env:"MAX_RETRIES" envDefault:"3"`
	RetryDelay time.Duration `env:"RETRY_DELAY" envDefault:"1s"`
}

// MCPConfig holds MCP server configuration
type MCPConfig struct {
	// Server info
	Name    string `env:"NAME" envDefault:"OPC-UA MCP Server"`
	Version string `env:"VERSION" envDefault:"1.0.0"`

	// Capabilities
	EnableTools     bool `env:"ENABLE_TOOLS" envDefault:"true"`
	EnableResources bool `env:"ENABLE_RESOURCES" envDefault:"true"`
	EnablePrompts   bool `env:"ENABLE_PROMPTS" envDefault:"false"`

	// HTTP endpoint path
	HTTPPath string `env:"HTTP_PATH" envDefault:"/mcp"`
}

// SearchConfig holds search and discovery configuration
type SearchConfig struct {
	// Node discovery settings
	EnableDiscovery   bool          `env:"ENABLE_DISCOVERY" envDefault:"true"`
	DiscoveryInterval time.Duration `env:"DISCOVERY_INTERVAL" envDefault:"30s"`
	DiscoveryRootNode string        `env:"DISCOVERY_ROOT_NODE" envDefault:"i=85"` // Objects folder
	MaxDiscoveryDepth int           `env:"MAX_DISCOVERY_DEPTH" envDefault:"10"`
	MaxNodesPerBrowse int           `env:"MAX_NODES_PER_BROWSE" envDefault:"10000"`

	// Search settings
	EnableSearch     bool    `env:"ENABLE_SEARCH" envDefault:"true"`
	SearchIndexPath  string  `env:"SEARCH_INDEX_PATH" envDefault:"./search_index"`
	SearchMaxResults int     `env:"SEARCH_MAX_RESULTS" envDefault:"100"`
	SearchMinScore   float64 `env:"SEARCH_MIN_SCORE" envDefault:"0.1"`

	// Cache settings. EnableCache is the master on/off switch for
	// internal/opcua.CachingClient's read-through caching (false ->
	// always live, matching pre-Unit-3 behavior exactly). TTLs live on
	// StoreConfig (TypeInfoTTL/BrowseTTL) since they're properties of the
	// persistent store, not of search/discovery.
	EnableCache bool `env:"ENABLE_CACHE" envDefault:"true"`
}

// StoreConfig holds persistent store (bbolt) configuration
type StoreConfig struct {
	// DBPath is the bbolt database file path.
	DBPath string `env:"DB_PATH" envDefault:"mcp_opcua_store.db"`

	// OpenTimeout bounds how long Open waits to acquire the file lock -
	// mandatory non-zero, or a stale lock from a prior ungraceful shutdown
	// hangs bbolt.Open (and hence stdio startup) indefinitely.
	OpenTimeout time.Duration `env:"OPEN_TIMEOUT" envDefault:"5s"`

	// TypeInfoTTL is how long a cached node-type-info entry is considered fresh.
	TypeInfoTTL time.Duration `env:"TYPEINFO_TTL" envDefault:"24h"`

	// BrowseTTL is how long a cached browse result is considered fresh.
	BrowseTTL time.Duration `env:"BROWSE_TTL" envDefault:"5m"`

	// BatchWindow/BatchMaxItems bound the subscription notification pump's
	// batching of values-bucket writes.
	BatchWindow   time.Duration `env:"BATCH_WINDOW" envDefault:"25ms"`
	BatchMaxItems int           `env:"BATCH_MAX_ITEMS" envDefault:"250"`

	// NotifyChanBuffer sizes the shared subscription notification channel.
	NotifyChanBuffer int `env:"NOTIFY_CHAN_BUFFER" envDefault:"1024"`
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{}

	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate transport mode
	if c.Server.Transport != "stdio" && c.Server.Transport != "http" {
		return &ValidationError{Field: "SERVER_TRANSPORT", Message: "must be 'stdio' or 'http'"}
	}

	// Validate auth mode
	validAuthModes := map[string]bool{
		"anonymous":   true,
		"username":    true,
		"certificate": true,
	}
	if !validAuthModes[c.OPCUA.AuthMode] {
		return &ValidationError{Field: "OPCUA_AUTH_MODE", Message: "must be 'anonymous', 'username', or 'certificate'"}
	}

	// Validate security policy
	validSecurityPolicies := map[string]bool{
		"None":                  true,
		"Basic128Rsa15":         true,
		"Basic256":              true,
		"Basic256Sha256":        true,
		"Aes128_Sha256_RsaOaep": true,
	}
	if !validSecurityPolicies[c.OPCUA.SecurityPolicy] {
		return &ValidationError{Field: "OPCUA_SECURITY_POLICY", Message: "invalid security policy"}
	}

	// Validate security mode
	validSecurityModes := map[string]bool{
		"None":           true,
		"Sign":           true,
		"SignAndEncrypt": true,
	}
	if !validSecurityModes[c.OPCUA.SecurityMode] {
		return &ValidationError{Field: "OPCUA_SECURITY_MODE", Message: "invalid security mode"}
	}

	// Validate username/password for username auth
	if c.OPCUA.AuthMode == "username" {
		if c.OPCUA.Username == "" {
			return &ValidationError{Field: "OPCUA_USERNAME", Message: "required when AUTH_MODE is 'username'"}
		}
		if c.OPCUA.Password == "" {
			return &ValidationError{Field: "OPCUA_PASSWORD", Message: "required when AUTH_MODE is 'username'"}
		}
	}

	// Validate certificate files for certificate auth
	if c.OPCUA.AuthMode == "certificate" {
		if c.OPCUA.CertFile == "" {
			return &ValidationError{Field: "OPCUA_CERT_FILE", Message: "required when AUTH_MODE is 'certificate'"}
		}
		if c.OPCUA.KeyFile == "" {
			return &ValidationError{Field: "OPCUA_KEY_FILE", Message: "required when AUTH_MODE is 'certificate'"}
		}
	}

	return nil
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
