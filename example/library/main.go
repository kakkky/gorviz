// library はライブラリ型 (gorviz.Start で agent をアプリに埋め込む形) の動作確認用の観測対象。pprof の HTTP は公開しない。
// gorviz-server の agent の口 (-agent-addr) に、アプリの中から直接つなぐ
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/kakkky/gorviz"
)

func main() {
	server := flag.String("server", "localhost:7071", "address where the gorviz server waits for agents")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := gorviz.Start(ctx, gorviz.Options{Server: *server, Name: "library-demo", DumpInterval: 5 * time.Second, LeakCheckEvery: 2})
	if err != nil {
		slog.Error("start gorviz agent", slog.Any("err", err))
		os.Exit(1)
	}
	defer a.Stop()
	logger := slog.New(gorviz.NewSlogHandler(slog.NewJSONHandler(os.Stderr, nil)))

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
