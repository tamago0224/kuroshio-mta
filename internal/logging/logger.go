package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func New(level string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	lvl := parseLevel(level)
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
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
