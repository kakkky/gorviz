// gorviz-tui は GUI とおおむね同じ画面を端末で見る。server の API とだけ話す
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kakkky/gorviz/adapter/serverapi"
	"github.com/kakkky/gorviz/presentation/terminal"
)

func main() {
	server := flag.String("server", "http://localhost:7070", "URL of the gorviz server")
	agent := flag.String("agent", "", "name of the agent to show (default: the first connected one)")
	refresh := flag.Duration("refresh", 2*time.Second, "refresh interval")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := terminal.New(serverapi.NewClient(*server, *agent), *server, *refresh).Run(ctx); err != nil {
		slog.Error("gorviz-tui failed", slog.Any("err", err))
		os.Exit(1)
	}
}
