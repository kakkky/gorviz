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

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/adapter/agentrpc"
	"github.com/kakkky/gorviz/internal/config"
	"github.com/kakkky/gorviz/internal/driver/httpserver"
	"github.com/kakkky/gorviz/internal/driver/session"
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
	registry := session.NewRegistry(cfg)
	srv := httpserver.NewHTTPServer(cfg, httpserver.NewServerMux(registry))
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(srv.Run)
		sc.Go(func(ctx context.Context) error {
			return agentrpc.Accept(ctx, cfg.AgentAddr, func(h agentrpc.Hello) (*agentrpc.Client, bool) {
				return registry.Attach(ctx, h, func(f func(context.Context) error) { sc.Go(f) })
			})
		})
		return nil
	})
}
