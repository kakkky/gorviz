// leaky は gorviz の動作確認用の観測対象。CPU を使う worker と、受け手のいない channel で止まり続ける goroutine を作り続ける。
// ログは gorviz の slog ハンドラ経由で出すので、Explore でクリックするとログを書いた goroutine まで特定できる。
package main

import (
	"log/slog"
	"math/rand/v2"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/kakkky/gorviz/slogtrace"
)

var logger = slog.New(slogtrace.Wrap(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

func main() {
	for i := range 4 {
		go cpuWorker(i)
	}
	go leakForever()
	go shortLived()

	logger.Info("pprof server started", "addr", "http://localhost:6060/debug/pprof/")
	if err := http.ListenAndServe("localhost:6060", nil); err != nil {
		logger.Error("pprof server stopped", "err", err)
		os.Exit(1)
	}
}

func cpuWorker(id int) {
	for batch := 0; ; batch++ {
		n := 0
		for i := range 2_000_000 {
			n += i % 7
		}
		_ = n
		if batch%300 == 0 {
			logger.Info("batch done", "worker", id, "batch", batch)
		}
		time.Sleep(time.Duration(rand.IntN(5)) * time.Millisecond)
	}
}

func leakForever() {
	for i := 1; ; i++ {
		go leakedReceive()
		if i%10 == 0 {
			logger.Warn("spawned receivers that nobody sends to", "total", i)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func leakedReceive() {
	ch := make(chan int)
	<-ch
}

func shortLived() {
	for i := 0; ; i++ {
		done := make(chan struct{})
		go func() {
			time.Sleep(10 * time.Millisecond)
			if rand.IntN(200) == 0 {
				logger.Error("short-lived job failed", "job", i)
			}
			close(done)
		}()
		<-done
	}
}
