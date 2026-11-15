// gorviz-agent は対象の隣に置き、trace・goroutine dump・ログを取ってリングバッファに持つ。
// server へ外向きにつなぎ、軽いもの (ログ・dump) は自分から送り、trace は server に求められたら返す。画面は持たない
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kakkky/gorviz/internal/app/agent"
	"github.com/kakkky/gorviz/internal/app/agent/config"
)

const usage = `gorviz-agent records the Go runtime of targets serving net/http/pprof and serves it to a gorviz server.

Usage:
  gorviz-agent [flags]    observe -target (comma separated targets for the daemon type)

Run "gorviz-agent -h" for flags.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("gorviz-agent failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.NewConfig("gorviz-agent", args)
	if err != nil {
		fmt.Fprint(os.Stderr, usage)
		return err
	}
	return agent.RunTargets(ctx, cfg, os.Stdin)
}
