// gorviz-mcp は LLM エージェント向けの MCP サーバ (stdio)。server の API とだけ話す
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kakkky/gorviz/internal/app/mcp"
)

func main() {
	server := flag.String("server", "http://localhost:7070", "URL of the gorviz server")
	agent := flag.String("agent", "", "default agent (default: the first connected one)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// stdout は MCP のやり取りに使うので、ログは stderr に出す
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := mcp.Serve(ctx, os.Stdin, os.Stdout, *server, *agent); err != nil {
		slog.Error("gorviz-mcp failed", slog.Any("err", err))
		os.Exit(1)
	}
}
