// Package logging builds the single slog.Logger every binary starts with:
// JSON in production, human-readable text when LAZYCAKE_DEV=1.
package logging

import (
	"io"
	"log/slog"
)

// New returns a configured logger. dev=true selects a text handler at debug
// level; dev=false selects a JSON handler at info level, for production.
func New(w io.Writer, dev bool) *slog.Logger {
	level := slog.LevelInfo
	var handler slog.Handler
	if dev {
		level = slog.LevelDebug
		handler = slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	}
	return slog.New(handler)
}
