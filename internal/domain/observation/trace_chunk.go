package observation

import (
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/pkg/histogram"
)

type GoroutineID int64

type ProcID int64

// ThreadID はチャンクの中で現れた順に 0 から振り直した M の番号。OS のスレッドの ID は桁が大きく読みにくいため
type ThreadID int64

// TraceChunk は execution trace を数秒ごとに切り分けて整形した 1 つ分。チャンクの中の時刻は StartedAt からの経過時間
type TraceChunk struct {
	StartedAt           time.Time
	Duration            time.Duration
	Events              []StateChange
	Procs               []ProcLane
	Entries             map[GoroutineID]string
	GC                  []GCRange
	Heap                []HeapSample
	GCEnds              []GCEnd
	GoroutinesCreated   int
	GoroutinesDestroyed int
	GCCycles            int
	Threads             int
	LogMarks            []LogMark
	Stats               TraceStats
}

func (c TraceChunk) EndedAt() time.Time {
	return c.StartedAt.Add(c.Duration)
}

func (c TraceChunk) Overlaps(from, to time.Time) bool {
	return c.StartedAt.Before(to) && c.EndedAt().After(from)
}

func (c TraceChunk) Contains(t time.Time) bool {
	return !t.Before(c.StartedAt) && !t.After(c.EndedAt())
}

func (c TraceChunk) Offset(t time.Time) time.Duration {
	return t.Sub(c.StartedAt)
}

type StateChangeKind string

const (
	StateChangeGoroutine StateChangeKind = "goroutine"
	StateChangeProc      StateChangeKind = "proc"
)

type PlaceKind string

const (
	PlaceRunning     PlaceKind = "running"
	PlaceLocalQueue  PlaceKind = "local_run_queue"
	PlaceGlobalQueue PlaceKind = "global_run_queue"
	PlaceWaiting     PlaceKind = "waiting"
	PlaceSyscall     PlaceKind = "syscall"
	PlaceDead        PlaceKind = "dead"
)

const UnknownWaitReason = "unknown"

// Place の HasProc が false の syscall は、P を手放した M の上で syscall 中であること
type Place struct {
	Kind    PlaceKind
	Proc    ProcID
	HasProc bool
	Thread  ThreadID
	Reason  string
	Cgo     bool
}

type ProcStatus string

const (
	ProcIdle    ProcStatus = "idle"
	ProcRunning ProcStatus = "running"
)

// StateChange は G か P の状態の変化 1 つ。Function はスケジューラの内部を除いた止まった関数、Location はそれを呼んだ利用者のコードの場所。
// WokenBy は待機から起こしたのが別の G なら、その G と、そのとき G がいた関数と場所
type StateChange struct {
	At              time.Duration
	Kind            StateChangeKind
	Goroutine       GoroutineID
	Place           Place
	Function        string
	Location        string
	Created         bool
	WokenBy         GoroutineID
	WokenByFunction string
	WokenByLocation string
	Proc            ProcID
	ProcStatus      ProcStatus
	Thread          ThreadID
}

type ProcSpan struct {
	Goroutine GoroutineID
	Thread    ThreadID
	Start     time.Duration
	End       time.Duration
}

type ProcLane struct {
	Proc  ProcID
	Spans []ProcSpan
}

type GCRangeKind string

const (
	GCStopTheWorld GCRangeKind = "stw"
	GCMarkAssist   GCRangeKind = "mark_assist"
	GCMark         GCRangeKind = "mark"
)

type GCRange struct {
	Kind      GCRangeKind
	Goroutine GoroutineID
	Reason    string
	Start     time.Duration
	End       time.Duration
}

// ByGC は STW が GC 以外 (ReadMemStats・プロファイルの取得など) でも起きるので、理由で見分ける
func (r GCRange) ByGC() bool {
	return r.Kind != GCStopTheWorld || strings.HasPrefix(r.Reason, "GC ")
}

type HeapSample struct {
	At           time.Duration
	ObjectsBytes uint64
	GoalBytes    uint64
}

type GCEnd struct {
	At            time.Duration
	LiveHeapBytes uint64
	HeapGoalBytes uint64
}

type LogMark struct {
	Seq       uint64
	Goroutine GoroutineID
	At        time.Duration
}

// TraceStats はチャンクの全イベントから数えた集計
type TraceStats struct {
	Procs        int
	Runnable     time.Duration
	Waits        []WaitSite
	SchedLatency histogram.Histogram
	STW          histogram.Histogram
	GCMaxShare   float64
	ProcRunning  time.Duration
	ProcGC       time.Duration
}

// WaitSite の Total は G ごとに待った時間の合計、Count はチャンクの中で待ちに入った回数
type WaitSite struct {
	Kind     PlaceKind
	Reason   string
	Cgo      bool
	Entry    string
	Function string
	Location string
	Count    int
	Total    time.Duration
	Max      time.Duration
}
