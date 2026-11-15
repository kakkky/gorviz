// Package agentrpc は agent と server の間の口。接続はどちらも agent から server へ外向きに張り、向きごとに 1 本ずつ持つ。
//   - push の接続: 軽いもの (ログ・goroutine dump) を agent が数秒ごとに送る (agent が RPC の呼び出し側)
//   - pull の接続: 重い trace を server が必要なときだけ求める (net/rpc を逆向きに使い、server が呼び出し側)
//
// 対象の環境の内側に外からの入口を開けずに、trace を何をいつ運ぶかは server が決められる
package agentrpc

import (
	"time"

	"github.com/kakkky/gorviz/internal/domain"
)

type ConnKind string

const (
	ConnPush ConnKind = "push"
	ConnPull ConnKind = "pull"
)

// Hello は agent がつないだ直後に 1 行の JSON で送る自己紹介。Kind でその接続の向きを伝える
type Hello struct {
	Name   string   `json:"name"`
	Target string   `json:"target"`
	Kind   ConnKind `json:"kind"`
}

const (
	agentServiceName  = "Agent"
	ingestServiceName = "Ingest"
)

type Empty struct{}

// PushArgs は agent が送る軽いもの。前に送ったものから変わっていないものは nil にする
type PushArgs struct {
	Logs []domain.LogLine
	Dump *domain.GoroutineDump
}

// ChunkArgs の WithRaw は生の trace も返すか。保存するときだけ要る。
// IfNewerThan より新しい区切りがなければ NotModified を返し、同じ区切りを何度も運ばない
type ChunkArgs struct {
	At          time.Time
	WithRaw     bool
	IfNewerThan time.Time
}

type ChunkReply struct {
	Chunk       domain.TraceChunk
	OK          bool
	NotModified bool
}

type BetweenArgs struct {
	From    time.Time
	To      time.Time
	WithRaw bool
}

type ChunksReply struct {
	Chunks []domain.TraceChunk
}

type SeqArgs struct {
	Seq uint64
}

type MarkReply struct {
	Chunk domain.TraceChunk
	Mark  domain.TraceLogMark
	OK    bool
}

type SpanReply struct {
	Span domain.RecordingSpan
	OK   bool
}
