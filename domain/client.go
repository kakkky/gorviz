package domain

import (
	"context"
	"time"
)

// RuntimeClient は観測対象プロセスのランタイム情報の取得元。
// 今は net/http/pprof を外から叩く実装だけだが、対象に埋め込む agent に差し替えられるよう interface にしておく。
type RuntimeClient interface {
	FetchGoroutines(ctx context.Context) ([]Goroutine, error)
	// FetchGoroutinesWithLeakCheck は runtime にリーク検出つきの GC を走らせてから全 G を取り、到達不能な G に Leaked を立てる。
	// GC を 1 回起こすので毎回は呼ばない。対象が goroutineleak プロファイルを持たない (Go 1.26 未満など) とき ErrNotFound を返す。
	FetchGoroutinesWithLeakCheck(ctx context.Context) ([]Goroutine, error)
	// FetchTraceChunk は duration の間 execution trace を取る。trace は対象のプロセスで同時に 1 本しか取れない
	FetchTraceChunk(ctx context.Context, duration time.Duration) (TraceChunk, error)
}
