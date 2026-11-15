// gorviz-agent は対象の隣に置き、trace・goroutine dump・ログを取ってリングバッファに持つ。
// server へ外向きにつなぎ、軽いもの (ログ・dump) は自分から送り、trace は server に求められたら返す。画面は持たない
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/adapter/client"
	"github.com/kakkky/gorviz/internal/config"
	"github.com/kakkky/gorviz/internal/driver/agent"
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
	// 省いたら hostname にし、レプリカごとに別の名前にする (同じ名前の agent は server で 1 つに束ねられてしまう)
	if cfg.Name == "" {
		if cfg.Name, err = os.Hostname(); err != nil {
			return err
		}
	}
	// daemon 型: 同じホストの複数の対象を、対象ごとに別の agent として server につなぐ
	targets := strings.Split(cfg.TargetURL, ",")
	return scope.Run(ctx, func(sc *scope.Scope) error {
		for _, target := range targets {
			c := *cfg
			c.TargetURL = strings.TrimSpace(target)
			c.Name = nameOf(cfg.Name, c.TargetURL, len(targets) > 1)
			var logs []agent.LogSource
			if cfg.LogsFrom == "-" && len(targets) == 1 {
				logs = append(logs, agent.LogSource{R: os.Stdin, Source: "stdin", Echo: io.Discard})
			}
			a := agent.New(&c, client.NewRuntimeClient(&c))
			sc.Go(func(ctx context.Context) error { return a.Run(ctx, logs...) })
		}
		return nil
	})
}

// nameOf は agent の名前を決める。daemon 型で複数の対象を見るときは、対象ごとに別の名前になるよう host:port を足す
func nameOf(name, target string, many bool) string {
	if !many {
		return name
	}
	host := target
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		host = u.Host
	}
	return name + "/" + host
}
