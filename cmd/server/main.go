package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kakkky/gorviz/internal/app/server"
	"github.com/kakkky/gorviz/internal/app/server/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.NewConfig()
	if err != nil {
		slog.ErrorContext(ctx, "load config", slog.Any("err", err))
		os.Exit(1)
	}
	if err := server.Run(ctx, cfg); err != nil {
		slog.ErrorContext(ctx, "run server", slog.Any("err", err))
		os.Exit(1)
	}
}
