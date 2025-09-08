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

	// Configure output
	var output io.Writer
	switch strings.ToLower(cfg.LogOutput) {
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

	return &Logger{Logger: logger}, nil
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
