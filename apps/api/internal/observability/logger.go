// Package observability contains slog setup, the unified error envelope,
// and HTTP middleware (RequestID, Recovery, CORS, RateLimit, AccessLog, SecurityHeaders).
//
// The error envelope is uniform across the entire API:
//
//	{ "error": { "code": "STRING_CODE", "message": "...", "details": {} } }
package observability

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger returns a slog JSON logger writing to stdout at the given level.
//
// Levels: "debug" | "info" | "warn" | "error" (case-insensitive).
// Unknown values default to info.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     lvl,
		AddSource: false,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// Rename "msg" → "message" for consistency with error envelope.
			if a.Key == slog.MessageKey {
				a.Key = "message"
			}
			if a.Key == slog.TimeKey {
				a.Key = "ts"
			}
			if a.Key == slog.LevelKey {
				a.Key = "level"
			}
			return a
		},
	})
	return slog.New(h)
}