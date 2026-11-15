// library はライブラリ型 (gorvizagent をアプリに埋め込む形) の動作確認用の観測対象。pprof の HTTP は公開しない。
// gorviz-server の agent の口 (-agent-addr) に、アプリの中から直接つなぐ
package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/kakkky/gorviz/gorvizagent"
	"github.com/kakkky/gorviz/slogtrace"
)

func main() {
	server := flag.String("server", "localhost:7071", "address where the gorviz server waits for agents")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := gorvizagent.New(gorvizagent.Options{Server: *server, Name: "library-demo", Args: []string{"-dump-interval", "5s", "-leak-check-every", "2"}})
	if err != nil {
		slog.Error("start gorviz agent", slog.Any("err", err))
		os.Exit(1)
	}
	logger := slog.New(slogtrace.Wrap(slog.NewJSONHandler(io.MultiWriter(os.Stderr, a.LogWriter()), nil)))
	go func() {
		if err := a.Run(ctx); err != nil {
			slog.Error("gorviz agent stopped", slog.Any("err", err))
		}
	}()

	var mu sync.Mutex
	go func() {
		for ctx.Err() == nil {
			time.Sleep(4 * time.Second)
			mu.Lock()
			time.Sleep(1200 * time.Millisecond)
			mu.Unlock()
		}
	}()
	for i := 0; ctx.Err() == nil; i++ {
		go func() {
			start := time.Now()
			mu.Lock()
			time.Sleep(20 * time.Millisecond)
			mu.Unlock()
			logger.Info("request done", "request_id", i, "duration", time.Since(start))
		}()
		if i%5 == 0 {
			go func() { <-make(chan int) }()
		}
		time.Sleep(100 * time.Millisecond)
	}
}
