// gorviz は非対話の操作をまとめた CLI。ローカルで server と agent をまとめて起動したり、保存した録画を書き出したりする。
// 画面は持たない (見るのは GUI か gorviz-tui)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/adapter/serverapi"
)

const usage = `gorviz is the command line of gorviz.

Usage:
  gorviz up     [-addr] [-agent-addr] [-save-dir] [agent flags] [-- cmd args]
                start a gorviz server and a gorviz agent together (local use). With "-- cmd args"
                the agent starts the target itself and also reads the run queues reported by the runtime
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
	case "up":
		return up(ctx, args[1:])
	case "issues":
		return issues(ctx, args[1:])
	case "export":
		return export(ctx, args[1:])
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// up は server と agent を子プロセスとして起動する。どちらかが終わったら、もう片方も止める
func up(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gorviz up", flag.ContinueOnError)
	addr := fs.String("addr", "localhost:7070", "listen address of the GUI and the API")
	agentAddr := fs.String("agent-addr", "localhost:7071", "TCP address between the server and the agent")
	saveDir := fs.String("save-dir", "", "directory where the server saves recordings around issues")
	// agent のフラグはそのまま渡すので、ここでは知らないフラグで止まらないよう、自分のフラグだけを先頭から読む
	known, rest := splitKnown(args, map[string]bool{"addr": true, "agent-addr": true, "save-dir": true})
	if err := fs.Parse(known); err != nil {
		return err
	}
	server := []string{"-addr", *addr, "-agent-addr", *agentAddr}
	if *saveDir != "" {
		server = append(server, "-save-dir", *saveDir)
	}
	agent := append([]string{"-agent-addr", *agentAddr}, rest...)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(ctx context.Context) error {
			defer cancel()
			return runSibling(ctx, "gorviz-server", server)
		})
		sc.Go(func(ctx context.Context) error {
			defer cancel()
			// server が待ち受けを始めてからつなぐ。つながらなくても agent がつなぎ直すので、待つのは見た目のため
			time.Sleep(300 * time.Millisecond)
			return runSibling(ctx, "gorviz-agent", agent)
		})
		fmt.Fprintf(os.Stderr, "gorviz: open http://%s (or run gorviz-tui -server http://%s)\n", *addr, *addr)
		return nil
	})
}

// runSibling は同じディレクトリにある gorviz-server / gorviz-agent を起動する。なければ PATH から探す
func runSibling(ctx context.Context, name string, args []string) error {
	path := name
	if self, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(self), name); fileExists(p) {
			path = p
		}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
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

// splitKnown は先頭から自分の知っているフラグ (-name value / -name=value) だけを抜き出し、残りを順番どおり返す
func splitKnown(args []string, names map[string]bool) (known, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return known, append(rest, args[i:]...)
		}
		name := a
		for len(name) > 0 && name[0] == '-' {
			name = name[1:]
		}
		key, _, hasValue := strings.Cut(name, "=")
		if !names[key] {
			rest = append(rest, a)
			continue
		}
		known = append(known, a)
		if !hasValue && i+1 < len(args) {
			known = append(known, args[i+1])
			i++
		}
	}
	return known, rest
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
