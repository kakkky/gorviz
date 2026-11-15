// overhead は gorviz が対象にかける負荷の目安を測る。
//   - execution trace: 同じ仕事を trace なし / ありで回し、1 秒あたりの処理数の差を見る (gorviz の常時録画と同じく trace は取り続ける)
//   - goroutine dump: 待機中の G を増やしながら debug=2 の dump にかかる時間を測る (dump の間は全 G が止まる)
package main

import (
	"flag"
	"fmt"
	"io"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	duration := flag.Duration("d", 5*time.Second, "length of each measurement")
	rounds := flag.Int("rounds", 3, "rounds of without / with trace")
	flag.Parse()

	fmt.Printf("GOMAXPROCS=%d, %s per measurement, %d rounds\n", runtime.GOMAXPROCS(0), *duration, *rounds)
	var without, with float64
	for i := range *rounds {
		a := measure(*duration, false)
		b := measure(*duration, true)
		fmt.Printf("round %d: without trace %.0f ops/s, with trace %.0f ops/s (%.2f%% slower)\n", i+1, a, b, (1-b/a)*100)
		without += a
		with += b
	}
	fmt.Printf("execution trace overhead: %.2f%%\n\n", (1-with/without)*100)

	for _, n := range []int{1_000, 10_000, 100_000} {
		fmt.Printf("goroutine dump with %d waiting goroutines: %s\n", n, measureDump(n))
	}
}

// measure は CPU・channel・割り当て・ロックを混ぜた仕事を duration の間回し、1 秒あたりの処理数を返す
func measure(duration time.Duration, traced bool) float64 {
	if traced {
		if err := trace.Start(io.Discard); err != nil {
			panic(err)
		}
		defer trace.Stop()
	}
	var (
		ops  atomic.Int64
		mu   sync.Mutex
		wg   sync.WaitGroup
		stop = make(chan struct{})
		sink []byte
	)
	for range runtime.GOMAXPROCS(0) * 4 {
		wg.Go(func() {
			ch := make(chan int)
			go func() {
				for v := range ch {
					_ = v
				}
			}()
			defer close(ch)
			for {
				select {
				case <-stop:
					return
				default:
				}
				n := 0
				for i := range 2_000 {
					n += i % 7
				}
				ch <- n
				buf := make([]byte, 512)
				mu.Lock()
				sink = buf
				mu.Unlock()
				ops.Add(1)
			}
		})
	}
	time.Sleep(duration)
	close(stop)
	wg.Wait()
	_ = sink
	return float64(ops.Load()) / duration.Seconds()
}

func measureDump(n int) time.Duration {
	block := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() { <-block })
	}
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	if err := pprof.Lookup("goroutine").WriteTo(io.Discard, 2); err != nil {
		panic(err)
	}
	elapsed := time.Since(start)
	close(block)
	wg.Wait()
	return elapsed
}
