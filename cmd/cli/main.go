// gorviz は非対話の操作をまとめた CLI。検出した Issue を表示する。
// 画面は持たない (見るのは GUI)
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

	"github.com/kakkky/gorviz/internal/app/cli"
)

const usage = `gorviz is the command line of gorviz.

Usage:
  gorviz issues [-server] [-agent]    print the detected issues once
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "issues":
		return issues(ctx, args[1:])
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func issues(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gorviz issues", flag.ContinueOnError)
	server := fs.String("server", "http://localhost:7070", "URL of the gorviz server")
	agent := fs.String("agent", "", "name of the agent (default: the first connected one)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return cli.Issues(ctx, os.Stdout, *server, *agent)
}
