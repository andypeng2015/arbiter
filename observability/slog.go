package observability

import (
	"log/slog"
	"os"
	"strings"
)

// Standard field keys for structured logging.
const (
	KeyBundleName  = "bundle_name"
	KeyBundleID    = "bundle_id"
	KeyMode        = "mode"
	KeyStrategy    = "strategy"
	KeyWorker      = "worker"
	KeyArbiter     = "arbiter"
	KeySource      = "source"
	KeyHandlerKind = "handler_kind"
	KeyRequestID   = "request_id"
	KeyError       = "error"
)

// NewLogger creates a JSON slog logger at the given level.
func NewLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

// ParseLevel converts a string level name to slog.Level.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
