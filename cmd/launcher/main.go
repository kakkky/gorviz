// gorviz-launcher はローカルで server と agent をまとめて起動する
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
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		slog.Error("gorviz-launcher failed", slog.Any("err", err))
		os.Exit(1)
	}
}

// run は server と agent を子プロセスとして起動する。どちらかが終わったら、もう片方も止める
func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gorviz-launcher", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `Usage: gorviz-launcher [-addr] [-agent-addr] [-save-dir] [agent flags]

start a gorviz server and a gorviz agent together (local use). With "-logs -"
the agent reads the target's logs piped to stdin

`)
		fs.PrintDefaults()
	}
	addr := fs.String("addr", "localhost:7070", "listen address of the GUI and the API")
	agentAddr := fs.String("agent-addr", "localhost:7071", "TCP address between the server and the agent")
	saveDir := fs.String("save-dir", "", "directory where the server saves recordings around issues")
	if len(args) > 0 && (args[0] == "-h" || args[0] == "-help" || args[0] == "--help") {
		fs.Usage()
		return flag.ErrHelp
	}
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
		fmt.Fprintf(os.Stderr, "gorviz: open http://%s\n", *addr)
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

// splitKnown は先頭から自分の知っているフラグ (-name value / -name=value) だけを抜き出し、残りを順番どおり返す
func splitKnown(args []string, names map[string]bool) (known, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
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
