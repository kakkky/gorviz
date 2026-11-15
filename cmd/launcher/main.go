// gorviz-launcher はローカルで server と agent をまとめて起動する
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kakkky/gorviz/internal/app/launcher"
	"github.com/kakkky/gorviz/internal/app/launcher/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("gorviz-launcher failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.NewConfig("gorviz-launcher", args)
	if err != nil {
		return err
	}
	return launcher.Run(ctx, cfg, os.Stdin)
}
