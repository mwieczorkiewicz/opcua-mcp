package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the OPC-UA MCP Server
type Config struct {
	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// OPC-UA configuration
	OPCUA OPCUAConfig `mapstructure:"opcua"`

	// MCP configuration
	MCP MCPConfig `mapstructure:"mcp"`

	// Search and discovery configuration
	Search SearchConfig `mapstructure:"search"`

	// Persistent store configuration (values/typeinfo/browse/subscriptions cache)
	Store StoreConfig `mapstructure:"store"`
}

// ServerConfig holds server-specific configuration
type ServerConfig struct {
	// HTTP server configuration
	HTTPPort string `mapstructure:"http_port"`

	// Transport mode: "stdio" or "http"
	Transport string `mapstructure:"transport"`

	// Logging configuration
	LogLevel     string `mapstructure:"log_level"`      // debug, info, warn, error
	LogFormat    string `mapstructure:"log_format"`     // json, text
	LogOutput    string `mapstructure:"log_output"`     // stdout, stderr, file
	LogFile      string `mapstructure:"log_file"`       // file path if LOG_OUTPUT=file
	LogAddSource bool   `mapstructure:"log_add_source"` // add source file/line info
}

// OPCUAConfig holds OPC-UA client configuration
type OPCUAConfig struct {
	// Connection settings
	Endpoint string `mapstructure:"endpoint"`

	// Authentication settings
	AuthMode   string `mapstructure:"auth_mode"` // anonymous, username, certificate
	Username   string `mapstructure:"username"`
	Password   string `mapstructure:"password"`
	CertFile   string `mapstructure:"cert_file"`
	KeyFile    string `mapstructure:"key_file"`
	ServerCert string `mapstructure:"server_cert"`

	// Security settings
	SecurityPolicy string `mapstructure:"security_policy"` // None, Basic128Rsa15, Basic256, Basic256Sha256, Aes128_Sha256_RsaOaep
	SecurityMode   string `mapstructure:"security_mode"`   // None, Sign, SignAndEncrypt

	// Connection settings
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
	SessionTimeout time.Duration `mapstructure:"session_timeout"`

	// Retry settings
	MaxRetries int           `mapstructure:"max_retries"`
	RetryDelay time.Duration `mapstructure:"retry_delay"`
}

// MCPConfig holds MCP server configuration
type MCPConfig struct {
	// Server info
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`

	// Capabilities
	EnableTools     bool `mapstructure:"enable_tools"`
	EnableResources bool `mapstructure:"enable_resources"`
	EnablePrompts   bool `mapstructure:"enable_prompts"`

	// HTTP endpoint path
	HTTPPath string `mapstructure:"http_path"`
}

// SearchConfig holds search and discovery configuration
type SearchConfig struct {
	// Node discovery settings
	EnableDiscovery   bool          `mapstructure:"enable_discovery"`
	DiscoveryInterval time.Duration `mapstructure:"discovery_interval"`
	DiscoveryRootNode string        `mapstructure:"discovery_root_node"` // Objects folder
	MaxDiscoveryDepth int           `mapstructure:"max_discovery_depth"`
	MaxNodesPerBrowse int           `mapstructure:"max_nodes_per_browse"`

	// Search settings
	EnableSearch     bool    `mapstructure:"enable_search"`
	SearchIndexPath  string  `mapstructure:"search_index_path"`
	SearchMaxResults int     `mapstructure:"search_max_results"`
	SearchMinScore   float64 `mapstructure:"search_min_score"`

	// Cache settings. EnableCache is the master on/off switch for
	// internal/opcua.CachingClient's read-through caching (false ->
	// always live, matching pre-Unit-3 behavior exactly). TTLs live on
	// StoreConfig (TypeInfoTTL/BrowseTTL) since they're properties of the
	// persistent store, not of search/discovery.
	EnableCache bool `mapstructure:"enable_cache"`
}

// StoreConfig holds persistent store (bbolt) configuration
type StoreConfig struct {
	// DBPath is the bbolt database file path.
	DBPath string `mapstructure:"db_path"`

	// OpenTimeout bounds how long Open waits to acquire the file lock -
	// mandatory non-zero, or a stale lock from a prior ungraceful shutdown
	// hangs bbolt.Open (and hence stdio startup) indefinitely.
	OpenTimeout time.Duration `mapstructure:"open_timeout"`

	// TypeInfoTTL is how long a cached node-type-info entry is considered fresh.
	TypeInfoTTL time.Duration `mapstructure:"typeinfo_ttl"`

	// BrowseTTL is how long a cached browse result is considered fresh.
	BrowseTTL time.Duration `mapstructure:"browse_ttl"`

	// BatchWindow/BatchMaxItems bound the subscription notification pump's
	// batching of values-bucket writes.
	BatchWindow   time.Duration `mapstructure:"batch_window"`
	BatchMaxItems int           `mapstructure:"batch_max_items"`

	// NotifyChanBuffer sizes the shared subscription notification channel.
	NotifyChanBuffer int `mapstructure:"notify_chan_buffer"`
}

// configFileEnvVar names the environment variable used to point Load at an
// explicit config file. It is intentionally unprefixed (not SERVER_*) since
// it configures how configuration itself is loaded.
const configFileEnvVar = "CONFIG_FILE"

// Load loads configuration from all supported sources, in ascending order
// of precedence: built-in defaults, an optional TOML/YAML/JSON/etc config
// file, then environment variables (SERVER_*, OPCUA_*, MCP_*, SEARCH_*,
// STORE_*). Environment variables always win over the config file, so
// existing env-var-only deployments keep working unchanged.
func Load() (*Config, error) {
	v := viper.New()

	// "." in a viper key (e.g. "server.http_port") maps to "_" in the
	// corresponding env var (SERVER_HTTP_PORT), which is what
	// AutomaticEnv+SetEnvKeyReplacer reproduces below.
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaults(v)

	if err := readConfigFile(v); err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("parsing configuration: %w", err)
	}

	return cfg, nil
}

// readConfigFile loads an optional config file. If CONFIG_FILE is set, that
// exact path is read (any read error, including "not found", is fatal). Otherwise
// viper looks for ./config.{yaml,yml,toml,json,...} and silently continues if
// none exists - a config file has always been optional, env vars are enough.
func readConfigFile(v *viper.Viper) error {
	if path := os.Getenv(configFileEnvVar); path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("reading config file %q: %w", path, err)
		}
		return nil
	}

	v.SetConfigName("config")
	v.AddConfigPath(".")

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("reading config file: %w", err)
		}
	}

	return nil
}

// setDefaults registers every config key's default value so that (a) viper
// knows about the key at all (required for AutomaticEnv to pick up its env
// var during Unmarshal) and (b) behavior matches pre-viper defaults exactly.
func setDefaults(v *viper.Viper) {
	v.SetDefault("server.http_port", "8080")
	v.SetDefault("server.transport", "stdio")
	v.SetDefault("server.log_level", "info")
	v.SetDefault("server.log_format", "json")
	v.SetDefault("server.log_output", "stdout")
	v.SetDefault("server.log_file", "")
	v.SetDefault("server.log_add_source", false)

	v.SetDefault("opcua.endpoint", "opc.tcp://localhost:4840")
	v.SetDefault("opcua.auth_mode", "anonymous")
	v.SetDefault("opcua.username", "")
	v.SetDefault("opcua.password", "")
	v.SetDefault("opcua.cert_file", "")
	v.SetDefault("opcua.key_file", "")
	v.SetDefault("opcua.server_cert", "")
	v.SetDefault("opcua.security_policy", "None")
	v.SetDefault("opcua.security_mode", "None")
	v.SetDefault("opcua.request_timeout", 30*time.Second)
	v.SetDefault("opcua.session_timeout", 60*time.Second)
	v.SetDefault("opcua.max_retries", 3)
	v.SetDefault("opcua.retry_delay", 1*time.Second)

	v.SetDefault("mcp.name", "OPC-UA MCP Server")
	v.SetDefault("mcp.version", "1.0.0")
	v.SetDefault("mcp.enable_tools", true)
	v.SetDefault("mcp.enable_resources", true)
	v.SetDefault("mcp.enable_prompts", false)
	v.SetDefault("mcp.http_path", "/mcp")

	v.SetDefault("search.enable_discovery", true)
	v.SetDefault("search.discovery_interval", 30*time.Second)
	v.SetDefault("search.discovery_root_node", "i=85")
	v.SetDefault("search.max_discovery_depth", 10)
	v.SetDefault("search.max_nodes_per_browse", 10000)
	v.SetDefault("search.enable_search", true)
	v.SetDefault("search.search_index_path", "./search_index")
	v.SetDefault("search.search_max_results", 100)
	v.SetDefault("search.search_min_score", 0.1)
	v.SetDefault("search.enable_cache", true)

	v.SetDefault("store.db_path", "mcp_opcua_store.db")
	v.SetDefault("store.open_timeout", 5*time.Second)
	v.SetDefault("store.typeinfo_ttl", 24*time.Hour)
	v.SetDefault("store.browse_ttl", 5*time.Minute)
	v.SetDefault("store.batch_window", 25*time.Millisecond)
	v.SetDefault("store.batch_max_items", 250)
	v.SetDefault("store.notify_chan_buffer", 1024)
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
