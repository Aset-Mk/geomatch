package logging

import (
	"log/slog"
	"os"
)

// New returns a JSON logger
func New(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
