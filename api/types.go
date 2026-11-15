// Package api は server の JSON の API の形。server の handler と、TUI・CLI・MCP のクライアントが同じ型を使う
package api

import (
	"time"

	"github.com/kakkky/gorviz/domain"
)

type AgentJSON struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Connected bool   `json:"connected"`
}

type OverviewJSON struct {
	At               time.Time             `json:"at"`
	DumpAt           time.Time             `json:"dumpAt"`
	Goroutines       int                   `json:"goroutines"`
	States           map[string]int        `json:"states"`
	GoroutineHistory []int                 `json:"goroutineHistory"`
	TopOrigins       []OriginJSON          `json:"topOrigins"`
	Scopes           []domain.ScopeSummary `json:"scopes"`
	HeapLiveBytes    uint64                `json:"heapLiveBytes"`
	HeapGoalBytes    uint64                `json:"heapGoalBytes"`
	GCPerSecond      float64               `json:"gcPerSecond"`
	CPUWaiting       float64               `json:"cpuWaiting"`
	Issues           int                   `json:"issues"`
	ErrorsLastMinute int                   `json:"errorsLastMinute"`
	SlowLastMinute   int                   `json:"slowLastMinute"`
	Recording        *domain.RecordingSpan `json:"recording,omitempty"`
}

type OriginJSON struct {
	Origin string `json:"origin"`
	Count  int    `json:"count"`
}

// SchedulerJSON は最新の観測窓の終わりの GMP。run queue の中身は trace からの推測
type SchedulerJSON struct {
	WindowStartedAt time.Time                `json:"windowStartedAt"`
	WindowEnd       time.Time                `json:"windowEnd"`
	Procs           []ProcJSON               `json:"procs"`
	GlobalRunQueue  []int64                  `json:"globalRunQueue"`
	Waiting         []WaitingJSON            `json:"waiting"`
	HandedOff       []int64                  `json:"handedOff"`
	Exited          int                      `json:"exitedInWindow"`
	Lanes           []ProcLaneJSON           `json:"lanes"`
	SchedTrace      *domain.SchedTraceReport `json:"schedtrace,omitempty"`
}

type ProcJSON struct {
	Proc             int64   `json:"proc"`
	Status           string  `json:"status"`
	Thread           int64   `json:"thread"`
	Running          int64   `json:"running"`
	RunningEntry     string  `json:"runningEntry"`
	RunningInSyscall bool    `json:"runningInSyscall"`
	LocalRunQueue    []int64 `json:"localRunQueue"`
}

type WaitingJSON struct {
	Entry      string  `json:"entry"`
	Reason     string  `json:"reason"`
	Goroutines []int64 `json:"goroutines"`
}

type ProcLaneJSON struct {
	Proc        int64     `json:"proc"`
	Utilization float64   `json:"utilization"`
	Busy        []float64 `json:"busy"`
	Goroutines  []int64   `json:"goroutines"`
}

type IssueJSON struct {
	Kind      string    `json:"kind"`
	Certainty string    `json:"certainty"`
	Subject   string    `json:"subject"`
	Summary   string    `json:"summary"`
	Location  string    `json:"location,omitempty"`
	Trend     []int     `json:"trend,omitempty"`
	Goroutine *int64    `json:"goroutine,omitempty"`
	At        time.Time `json:"at"`
	From      time.Time `json:"from"`
	// GoroutineQuery は /api/goroutines/{goroutine} に付ける問い合わせ (at / from)。代表の G を診断するのに使う
	GoroutineQuery string `json:"goroutineQuery,omitempty"`
	LogQuery       string `json:"logQuery,omitempty"`
}

type LogJSON struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Level    string    `json:"level"`
	Message  string    `json:"message"`
	Duration string    `json:"duration,omitempty"`
	// TraceLinked はログを書いた G を slogtrace の番号で特定できる (FACT) 行か。/logs/{id}/replay の画面で確かめられる
	TraceLinked bool   `json:"traceLinked"`
	Raw         string `json:"raw"`
}

type GoroutineJSON struct {
	Goroutine  int64             `json:"goroutine"`
	Entry      string            `json:"entry"`
	From       time.Time         `json:"from"`
	To         time.Time         `json:"to"`
	At         time.Time         `json:"at"`
	Leaked     bool              `json:"leaked"`
	Primary    string            `json:"primaryCause,omitempty"`
	Certainty  string            `json:"certainty,omitempty"`
	Function   string            `json:"stoppedIn,omitempty"`
	Location   string            `json:"location,omitempty"`
	WaitReason string            `json:"waitReason,omitempty"`
	CreatedBy  string            `json:"createdBy,omitempty"`
	Breakdown  map[string]string `json:"timeByState"`
	Unrecorded string            `json:"unrecorded"`
	// EvidenceURL は人間が根拠 (GMP 再生) を画面で確かめるための URL
	EvidenceURL string `json:"evidenceUrl"`
}
