// leaky は gorviz の動作確認用の観測対象。CPU を使う worker と、受け手のいない channel で止まり続ける goroutine を作り続ける。
// リクエストを模した処理は所要時間つきのログを出し、ときどきロック待ちで遅くなる (SLOW_REQUEST と BLOCKED の確認用)。
// ログは gorviz の slog ハンドラ経由で出すので、Logs でクリックするとログを書いた goroutine まで特定できる。
package main

import (
	"context"
	"flag"
	"log/slog"
	"math/rand/v2"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime/pprof"
	"sync"
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
	go holdLock()
	go requests()

	addr := flag.String("addr", "localhost:6060", "listen address of the pprof server")
	flag.Parse()
	logger.Info("pprof server started", "addr", "http://"+*addr+"/debug/pprof/")
	if err := http.ListenAndServe(*addr, nil); err != nil {
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

// mu はときどき長く握られ、その間に来たリクエストはロック待ちで遅くなる
var mu sync.Mutex

func holdLock() {
	for {
		time.Sleep(5 * time.Second)
		mu.Lock()
		time.Sleep(1500 * time.Millisecond)
		mu.Unlock()
	}
}

func requests() {
	for i := 0; ; i++ {
		// スコープの可視化の確認用に、リクエストの G に pprof label "scope" を付ける
		go pprof.Do(context.Background(), pprof.Labels("scope", "request"), func(context.Context) { handleRequest(i) })
		time.Sleep(100 * time.Millisecond)
	}
}

func handleRequest(id int) {
	start := time.Now()
	mu.Lock()
	time.Sleep(time.Duration(10+rand.IntN(30)) * time.Millisecond)
	mu.Unlock()
	logger.Info("request done", "path", "/users", "request_id", id, "duration", time.Since(start))
}
