package logger

import (
	"log/slog"
	"os"
)

func New(levelStr string) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(handler)
}
