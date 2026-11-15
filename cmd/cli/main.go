// gorviz は非対話の操作をまとめた CLI。検出した Issue を表示したり、保存した録画を書き出したりする。
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
	"path/filepath"
	"syscall"

	"github.com/kakkky/gorviz/internal/adapter/serverapi"
)

const usage = `gorviz is the command line of gorviz.

Usage:
  gorviz issues [-server] [-agent]           print the detected issues once
  gorviz export [-server] [-agent] [-out]    write the saved recordings as trace files for "go tool trace"
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
	case "export":
		return export(ctx, args[1:])
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
	list, err := serverapi.NewClient(*server, *agent).Issues(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("no issues detected")
	}
	for _, i := range list {
		fmt.Printf("%-16s %-9s %s\n  %s\n", i.Kind, i.Certainty, i.Summary, i.Subject)
		if i.Goroutine != nil {
			fmt.Printf("  diagnose: %s/goroutines/%d?%s\n", *server, *i.Goroutine, i.GoroutineQuery)
		}
	}
	return nil
}

func export(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gorviz export", flag.ContinueOnError)
	server := fs.String("server", "http://localhost:7070", "URL of the gorviz server")
	agent := fs.String("agent", "", "name of the agent (default: the first connected one)")
	out := fs.String("out", "gorviz-recordings", "output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c := serverapi.NewClient(*server, *agent)
	recs, err := c.Recordings(ctx)
	if err != nil {
		return err
	}
	for _, r := range recs {
		dir := filepath.Join(*out, r.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for n := range r.Chunks {
			raw, err := c.RecordingChunk(ctx, r.ID, n)
			if err != nil {
				return err
			}
			p := filepath.Join(dir, fmt.Sprintf("chunk-%d.trace", n))
			if err := os.WriteFile(p, raw, 0o644); err != nil {
				return err
			}
		}
		fmt.Printf("%s: %d chunks → %s (go tool trace %s/chunk-0.trace)\n", r.ID, r.Chunks, dir, dir)
	}
	if len(recs) == 0 {
		fmt.Println("no saved recordings")
	}
	return nil
}
