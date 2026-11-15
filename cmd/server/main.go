// gorviz-server は agent を束ね、診断・保存と、GUI・JSON の API を受け持つ。agent からの接続を待ち受ける
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kakkky/gorviz/internal/app/server"
	"github.com/kakkky/gorviz/internal/app/server/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("gorviz-server failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.NewConfig("gorviz-server", args)
	if err != nil {
		return err
	}
	return server.Run(ctx, cfg)
}
