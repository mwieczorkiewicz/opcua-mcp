package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
)

// Logger wraps slog.Logger with additional functionality
type Logger struct {
	*slog.Logger
}

// New creates a new logger based on configuration
func New(cfg *config.ServerConfig) (*Logger, error) {
	// Parse log level
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Configure output. stdio transport carries the MCP JSON-RPC wire protocol on
	// stdout, so logging there is always forced to stderr regardless of LogOutput.
	envVal, ok := os.LookupEnv(logOutputEnvVar)
	explicitStdoutOverride := ok && strings.EqualFold(envVal, "stdout")
	effectiveOutput, warning := resolveLogOutput(cfg.Transport, cfg.LogOutput, explicitStdoutOverride)

	var output io.Writer
	switch strings.ToLower(effectiveOutput) {
	case "stderr":
		output = os.Stderr
	case "file":
		if cfg.LogFile == "" {
			output = os.Stdout
		} else {
			file, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				return nil, err
			}
			output = file
		}
	default: // stdout
		output = os.Stdout
	}

	// Configure format
	var handler slog.Handler
	switch strings.ToLower(cfg.LogFormat) {
	case "text":
		handler = slog.NewTextHandler(output, &slog.HandlerOptions{
			Level:     level,
			AddSource: cfg.LogAddSource,
		})
	default: // json
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level:     level,
			AddSource: cfg.LogAddSource,
		})
	}

	// Create logger
	logger := slog.New(handler)

	// Set as default logger
	slog.SetDefault(logger)

	if warning != "" {
		logger.Warn(warning)
	}

	return &Logger{Logger: logger}, nil
}

// logOutputEnvVar is the env var name backing ServerConfig.LogOutput
// (envPrefix "SERVER_" + env:"LOG_OUTPUT"), used to detect an explicit
// user override as opposed to the envDefault taking effect.
const logOutputEnvVar = "SERVER_LOG_OUTPUT"

// resolveLogOutput determines the effective log output destination for a given
// transport/configured-output combination, forcing stderr whenever transport is
// stdio (stdout there carries the MCP JSON-RPC wire protocol). It returns a
// non-empty warning only when the caller explicitly set LogOutput=stdout via
// the environment (as opposed to the envDefault), so the override is surfaced
// once rather than silently swallowed.
func resolveLogOutput(transport, configuredOutput string, explicitStdoutOverride bool) (effective string, warning string) {
	if strings.EqualFold(transport, "stdio") && strings.EqualFold(configuredOutput, "stdout") {
		if explicitStdoutOverride {
			return "stderr", "SERVER_LOG_OUTPUT=stdout is ignored in stdio transport mode; logging to stderr to protect the JSON-RPC stream"
		}
		return "stderr", ""
	}
	return configuredOutput, ""
}

// WithContext returns a logger with context
func (l *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{Logger: l.Logger}
}

// WithFields returns a logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	args := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return &Logger{Logger: l.Logger.With(args...)}
}

// WithField returns a logger with a single additional field
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{Logger: l.Logger.With(key, value)}
}

// WithError returns a logger with an error field
func (l *Logger) WithError(err error) *Logger {
	return &Logger{Logger: l.Logger.With("error", err)}
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, args ...interface{}) {
	l.Logger.Debug(msg, args...)
}

// Info logs an info message
func (l *Logger) Info(msg string, args ...interface{}) {
	l.Logger.Info(msg, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, args ...interface{}) {
	l.Logger.Warn(msg, args...)
}

// Error logs an error message
func (l *Logger) Error(msg string, args ...interface{}) {
	l.Logger.Error(msg, args...)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(msg string, args ...interface{}) {
	l.Logger.Error(msg, args...)
	os.Exit(1)
}

// Global logger instance
var globalLogger *Logger

// Init initializes the global logger
func Init(cfg *config.ServerConfig) error {
	logger, err := New(cfg)
	if err != nil {
		return err
	}
	globalLogger = logger
	return nil
}

// Get returns the global logger
func Get() *Logger {
	if globalLogger == nil {
		// Fallback to default logger
		globalLogger = &Logger{Logger: slog.Default()}
	}
	return globalLogger
}

// Global convenience functions
func Debug(msg string, args ...interface{}) {
	Get().Debug(msg, args...)
}

func Info(msg string, args ...interface{}) {
	Get().Info(msg, args...)
}

func Warn(msg string, args ...interface{}) {
	Get().Warn(msg, args...)
}

func Error(msg string, args ...interface{}) {
	Get().Error(msg, args...)
}

func Fatal(msg string, args ...interface{}) {
	Get().Fatal(msg, args...)
}

func WithFields(fields map[string]interface{}) *Logger {
	return Get().WithFields(fields)
}

func WithField(key string, value interface{}) *Logger {
	return Get().WithField(key, value)
}

func WithError(err error) *Logger {
	return Get().WithError(err)
}
