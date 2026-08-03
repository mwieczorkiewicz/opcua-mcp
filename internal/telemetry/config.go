package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/logger"
)

// defaultFlushInterval is how often aggregated counters are flushed as a
// single session_summary event, absent TELEMETRY_FLUSH_INTERVAL.
const defaultFlushInterval = 10 * time.Minute

// Config resolves the opt-out decision and flush cadence. It intentionally
// doesn't route through internal/config's viper-based Config struct: the two
// opt-out variables below (DO_NOT_TRACK, OPCUA_MCP_TELEMETRY) don't follow
// this project's SERVER_*/OPCUA_*/MCP_*/SEARCH_*/STORE_* dot-to-underscore
// namespacing by design - one is a cross-project community convention, the
// other names the whole binary rather than a config section - so resolving
// them via plain os.Getenv here is simpler than teaching viper a second,
// incompatible naming scheme for three variables.
// Transport/AuthMode/SecurityPolicy/ServerVersion are static session
// dimensions the caller (cmd/opcua-mcp.go) fills in from the already-loaded
// application config after calling LoadConfig - they aren't themselves
// resolved from the environment here, since they already exist as
// SERVER_TRANSPORT/OPCUA_AUTH_MODE/OPCUA_SECURITY_POLICY/MCP_VERSION on the
// main internal/config.Config and shouldn't be parsed a second time.
type Config struct {
	Enabled       bool
	FlushInterval time.Duration
	AnonID        string

	Transport      string
	AuthMode       string
	SecurityPolicy string
	ServerVersion  string
}

// LoadConfig resolves telemetry configuration from the environment.
func LoadConfig() Config {
	return Config{
		Enabled:       resolveEnabled(),
		FlushInterval: resolveFlushInterval(),
		AnonID:        resolveAnonID(),
	}
}

// resolveEnabled implements the opt-out precedence: DO_NOT_TRACK (the
// community convention, https://consoledonottrack.com) or
// OPCUA_MCP_TELEMETRY=false disable telemetry; anything else, including both
// unset, leaves it on by default.
func resolveEnabled() bool {
	if v, ok := os.LookupEnv("DO_NOT_TRACK"); ok {
		if b, ok := parseBool(v); ok {
			return !b
		}
	}
	if v, ok := os.LookupEnv("OPCUA_MCP_TELEMETRY"); ok {
		if b, ok := parseBool(v); ok {
			return b
		}
	}
	return true
}

// parseBool accepts the same case-insensitive truthy/falsy spellings for
// both DO_NOT_TRACK and OPCUA_MCP_TELEMETRY so neither var surprises a user
// coming from the other's convention.
func parseBool(v string) (value bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func resolveFlushInterval() time.Duration {
	v := os.Getenv("TELEMETRY_FLUSH_INTERVAL")
	if v == "" {
		return defaultFlushInterval
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		logger.Debug("telemetry: invalid TELEMETRY_FLUSH_INTERVAL, using default", "value", v, "default", defaultFlushInterval)
		return defaultFlushInterval
	}
	return d
}

// resolveAnonID loads a persisted random anonymous ID, generating and
// persisting one on first run. If the config directory can't be read or
// written for any reason, it falls back to an ephemeral in-memory UUID
// rather than failing startup - the ID existing at all is never load-bearing
// for server correctness.
func resolveAnonID() string {
	path, ok := anonIDPath()
	if !ok {
		return uuid.NewString()
	}

	if data, err := os.ReadFile(path); err == nil {
		if id, err := uuid.Parse(strings.TrimSpace(string(data))); err == nil {
			return id.String()
		}
	}

	id := uuid.NewString()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		logger.Debug("telemetry: could not create config dir, using ephemeral anon ID", "error", err)
		return id
	}
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		logger.Debug("telemetry: could not persist anon ID, using ephemeral anon ID", "error", err)
	}
	return id
}

// anonIDPath returns $XDG_CONFIG_HOME/opcua-mcp/telemetry_id, falling back
// to $HOME/.config (the XDG default) if XDG_CONFIG_HOME is unset.
func anonIDPath() (string, bool) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "opcua-mcp", "telemetry_id"), true
}
