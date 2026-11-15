// Package agentrpc は agent と server の間の口。agent が server へ外向きに TCP でつなぎ、
// その接続の上で server が RPC の呼び出し側になる (net/rpc を逆向きに使う)。
// 対象の環境の内側に外からの入口を開けずに、何をいつ取るかを server が決められる (pull)
package agentrpc

import (
	"time"

	"github.com/kakkky/gorviz/domain"
)

// Hello は agent がつないだ直後に 1 行の JSON で送る自己紹介。以降はその接続の上で server からの呼び出しを受ける
type Hello struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

const serviceName = "Agent"

type Empty struct{}

type DumpReply struct {
	Dump domain.GoroutineDump
	OK   bool
}

type LogsArgs struct {
	After domain.LogLineID
	Limit int
}

type LogsReply struct {
	Lines []domain.LogLine
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

type SchedTraceReply struct {
	Report domain.SchedTraceReport
	OK     bool
}
