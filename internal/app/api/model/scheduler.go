package model

import "time"

type ProcID int64

type ThreadID int64

// SchedSpan は「G が M の上で P を持って走っていた区間」。Start / End は観測窓の先頭からの経過時間。
type SchedSpan struct {
	Goroutine GoroutineID
	Thread    ThreadID
	Start     time.Duration
	End       time.Duration
}

type ProcLane struct {
	Proc  ProcID
	Spans []SchedSpan
}

// SchedulerTimeline は execution trace から組み立てた、観測窓の中で P ごとに何が走ったかの記録。
type SchedulerTimeline struct {
	Window              time.Duration
	Procs               []ProcLane
	Threads             []ThreadID
	GoroutinesCreated   int
	GoroutinesDestroyed int
	// Events は再生用の状態変化の列 (At の昇順)。量が多すぎるときは打ち切り、EventsTruncated を立てる
	Events          []SchedEvent
	EventsTruncated bool
	// Entries は G ごとの入口の関数 (go 文で起動された関数)。trace の生成時のスタックと goroutine dump から集める
	Entries map[GoroutineID]string
	// GC は窓の中で GC が G を止めた / 手伝わせた区間 (At の昇順)
	GC []GCRange
	// Heap はヒープの大きさと GC の目標値の推移。trace に載る値をそのまま並べる
	Heap []HeapSample
	// GCCycles は窓の中で始まった GC の回数
	GCCycles int
}

type GCRangeKind string

const (
	// GCStopTheWorld は全 G が止められた区間。どの G にとっても GC の時間になる
	GCStopTheWorld GCRangeKind = "stw"
	// GCMarkAssist は Goroutine が割り当ての代わりに GC の作業を手伝わされた区間
	GCMarkAssist GCRangeKind = "mark_assist"
)

type GCRange struct {
	Kind      GCRangeKind
	Goroutine GoroutineID
	Start     time.Duration
	End       time.Duration
}

type HeapSample struct {
	At        time.Duration
	LiveBytes uint64
	GoalBytes uint64
}
