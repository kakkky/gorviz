// gorviz は、手元で Go のアプリを動かし、そのあいだ Go ランタイムが健康かを診断する。デーモンとして起動されたときは server、それ以外は cli
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kakkky/gorviz/internal/app/cli"
	cliconfig "github.com/kakkky/gorviz/internal/app/cli/config"
	"github.com/kakkky/gorviz/internal/app/server"
	serverconfig "github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if serverapi.IsDaemon() {
		cfg, err := serverconfig.NewConfig()
		if err != nil {
			slog.ErrorContext(ctx, "load config", slog.Any("err", err))
			os.Exit(1)
		}
		if err := server.Run(ctx, cfg); err != nil {
			slog.ErrorContext(ctx, "run server", slog.Any("err", err))
			os.Exit(1)
		}
		return
	}
	cfg, err := cliconfig.NewConfig()
	if err != nil {
		slog.ErrorContext(ctx, "load config", slog.Any("err", err))
		os.Exit(1)
	}
	if err := cli.Run(ctx, cfg); err != nil {
		slog.ErrorContext(ctx, "run cli", slog.Any("err", err))
		os.Exit(1)
	}
}
