// gorviz-agent は対象の隣に置き、trace・goroutine dump・ログを取ってリングバッファに持つ。
// server へ外向きにつなぎ、server に求められたら返す。画面は持たない
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

	"github.com/kakkky/gorviz/adapter/client"
	"github.com/kakkky/gorviz/adapter/schedtrace"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/driver/agent"
	"github.com/kakkky/gorviz/driver/childprocess"
)

const usage = `gorviz-agent records the Go runtime of targets serving net/http/pprof and serves it to a gorviz server.

Usage:
  gorviz-agent [flags]                       observe -target (comma separated targets for the daemon type)
  gorviz-agent [flags] -- cmd args           start cmd with GODEBUG=schedtrace and observe it (local only)

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
	if len(cfg.Command) > 0 {
		return runChild(ctx, cfg)
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
			a := agent.New(&c, client.NewRuntimeClient(&c), schedtrace.NewStore())
			sc.Go(func(ctx context.Context) error { return a.Run(ctx, logs...) })
		}
		return nil
	})
}

// runChild は対象を子プロセスとして起動し、その標準エラー出力から schedtrace を読みながら観測する。対象が終了したら agent も止める
func runChild(ctx context.Context, cfg *config.Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cfg.Name = nameOf(cfg.Name, cfg.TargetURL, false)
	child, err := childprocess.Start(ctx, cfg.Command)
	if err != nil {
		return err
	}
	store := schedtrace.NewStore()
	a := agent.New(cfg, client.NewRuntimeClient(cfg), store)
	// stderr には schedtrace と対象のログが混ざって流れてくるので、schedtrace を抜いた残りをログとして取り込む
	stderrLogs, stderrLogsW := io.Pipe()
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(context.Context) error {
			err := store.Consume(child.Stderr(), stderrLogsW)
			stderrLogsW.CloseWithError(err)
			return err
		})
		sc.Go(func(context.Context) error {
			defer cancel()
			if err := child.Wait(); err != nil && ctx.Err() == nil {
				return fmt.Errorf("target exited: %w", err)
			}
			return nil
		})
		sc.Go(func(ctx context.Context) error {
			return a.Run(ctx,
				agent.LogSource{R: child.Stdout(), Source: "stdout", Echo: os.Stdout},
				agent.LogSource{R: stderrLogs, Source: "stderr", Echo: os.Stderr},
			)
		})
		return nil
	})
}

// nameOf は agent の名前を決める。指定がなければ対象の host:port。daemon 型で複数の対象を見るときは、指定した名前に host:port を足す
func nameOf(name, target string, many bool) string {
	host := target
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		host = u.Host
	}
	switch {
	case name == "":
		return host
	case many:
		return name + "/" + host
	default:
		return name
	}
}
