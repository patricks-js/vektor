package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a structured JSON slog logger safe for daemon stderr/journald output.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}

// ParseLogLevel parses a case-insensitive log level string ("debug", "info", "warn", "error").
func ParseLogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
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
