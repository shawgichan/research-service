package logger

import (
	"log/slog"
	"os"
)

type AppLogger struct {
	*slog.Logger
}

func New() *AppLogger {
	var handler slog.Handler
	env := os.Getenv("ENVIRONMENT")
	if env == "development" {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
	} else {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}
	logger := slog.New(handler)
	return &AppLogger{Logger: logger}
}

func (l *AppLogger) Fatal(msg string, err error, args ...any) {
	allArgs := append(args, "error", err)
	l.Error(msg, allArgs...)
	os.Exit(1)
}
