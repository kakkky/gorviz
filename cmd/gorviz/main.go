package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/adapter/schedtrace"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/driver/childprocess"
)

const usage = `gorviz visualizes the Go runtime (G / M / P and goroutine leaks) of a process serving net/http/pprof.

Usage:
  gorviz top   [flags]                show it in the terminal
  gorviz serve [flags]                serve it as a web GUI
  gorviz run   [flags] -- cmd args    start cmd with GODEBUG=schedtrace and show it together with
                                      the run queue lengths reported by the runtime (local only)

Run "gorviz <command> -h" for flags.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("gorviz failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return flag.ErrHelp
	}
	cmd, args := args[0], args[1:]

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.NewConfig("gorviz "+cmd, args)
	if err != nil {
		return err
	}
	var logs []logSource
	if cfg.LogsFrom == "-" {
		logs = append(logs, logSource{r: os.Stdin, source: "stdin", echo: io.Discard})
	}
	switch cmd {
	case "top":
		return InitTopApp(cfg, schedtrace.NewStore()).Run(ctx, logs...)
	case "serve":
		return InitWebApp(cfg, schedtrace.NewStore()).Run(ctx, logs...)
	case "run":
		return runWithSchedTrace(ctx, cfg)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// runWithSchedTrace は対象を子プロセスとして起動し、その標準エラー出力から schedtrace を読みながら UI を出す。
// 対象が終了したら UI も閉じる。
func runWithSchedTrace(ctx context.Context, cfg *config.Config) error {
	if cfg.UI != "web" && cfg.UI != "top" {
		return fmt.Errorf(`-ui must be "web" or "top", got %q`, cfg.UI)
	}
	store := schedtrace.NewStore()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	child, err := childprocess.Start(ctx, cfg.Command)
	if err != nil {
		return err
	}
	// 対象のログはそのまま端末にも流す。top 表示中は画面を崩すので流さない
	var stdoutEcho, stderrEcho io.Writer = os.Stdout, os.Stderr
	if cfg.UI == "top" {
		stdoutEcho, stderrEcho = io.Discard, io.Discard
	}
	// stderr には schedtrace と対象のログが混ざって流れてくるので、schedtrace を抜いた残りをログとして取り込む
	stderrLogs, stderrLogsW := io.Pipe()
	var app *App
	if cfg.UI == "top" {
		app = InitTopApp(cfg, store)
	} else {
		app = InitWebApp(cfg, store)
	}
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
			return app.Run(ctx,
				logSource{r: child.Stdout(), source: "stdout", echo: stdoutEcho},
				logSource{r: stderrLogs, source: "stderr", echo: stderrEcho},
			)
		})
		return nil
	})
}
