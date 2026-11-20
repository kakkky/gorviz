// Package domain は agent の側の口 (対象から取る・持つ・server に渡す)。判定は持たない (server の domain が持つ)
package domain

import (
	"context"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"io"
	"time"
)

// RuntimeClient は観測対象プロセスのランタイム情報の取得元。
// 今は net/http/pprof を外から叩く実装だけだが、対象に埋め込む agent に差し替えられるよう interface にしておく。
type RuntimeClient interface {
	FetchGoroutines(ctx context.Context) ([]model.Goroutine, error)
	// FetchGoroutinesWithLeakCheck は runtime にリーク検出つきの GC を走らせてから全 G を取り、到達不能な G に Leaked を立てる。
	// GC を 1 回起こすので毎回は呼ばない。対象が goroutineleak プロファイルを持たない (Go 1.26 未満など) とき ErrNotFound を返す。
	FetchGoroutinesWithLeakCheck(ctx context.Context) ([]model.Goroutine, error)
	// FetchTraceChunk は duration の間 execution trace を取る。trace は対象のプロセスで同時に 1 本しか取れない
	FetchTraceChunk(ctx context.Context, duration time.Duration) (model.TraceChunk, error)
}

// RuntimeParser は pprof の goroutine dump と execution trace を読む。RuntimeClient の実装が使う
type RuntimeParser interface {
	ParseGoroutineDump(r io.Reader) ([]model.Goroutine, error)
	ParseTrace(raw []byte, receivedAt, countFrom time.Time) (model.TraceChunk, error)
}

type LogParser interface {
	Parse(raw string, source string, receivedAt time.Time) model.LogLine
}

// TraceRecording は取った区切りを server に送るまで持つ
type TraceRecording interface {
	Append(chunk model.TraceChunk)
	// Since は after より後に始まった区切りを、古い順に limit 個まで返す
	Since(after time.Time, limit int) []model.TraceChunk
}

type LogRecording interface {
	Append(line model.LogLine) model.LogLine
	Snapshot() []model.LogLine
	Get(id model.LogLineID) (model.LogLine, bool)
}
