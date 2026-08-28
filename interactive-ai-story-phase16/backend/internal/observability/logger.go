package observability

import (
	"log/slog"
	"os"
)

func NewLogger(appEnv string) *slog.Logger {
	if appEnv == "development" {
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
