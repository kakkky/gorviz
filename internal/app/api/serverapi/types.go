package serverapi

import (
	"github.com/kakkky/gorviz/internal/app/api/model"
	"time"
)

type AgentJSON struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Connected bool   `json:"connected"`
}

type OverviewJSON struct {
	At               time.Time            `json:"at"`
	DumpAt           time.Time            `json:"dumpAt"`
	Goroutines       int                  `json:"goroutines"`
	States           map[string]int       `json:"states"`
	GoroutineHistory []int                `json:"goroutineHistory"`
	TopOrigins       []OriginJSON         `json:"topOrigins"`
	Scopes           []ScopeJSON          `json:"scopes"`
	HeapLiveBytes    uint64               `json:"heapLiveBytes"`
	HeapGoalBytes    uint64               `json:"heapGoalBytes"`
	GCPerSecond      float64              `json:"gcPerSecond"`
	CPUWaiting       float64              `json:"cpuWaiting"`
	Issues           int                  `json:"issues"`
	ErrorsLastMinute int                  `json:"errorsLastMinute"`
	SlowLastMinute   int                  `json:"slowLastMinute"`
	Recording        *model.RecordingSpan `json:"recording,omitempty"`
}

type OriginJSON struct {
	Origin string `json:"origin"`
	Count  int    `json:"count"`
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
	// TraceLinked はログを書いた G を ロガーの差し込み口 (gorviz.NewSlogHandler など) の番号で特定できる (FACT) 行か。/logs/{id}/replay の画面で確かめられる
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

// LogQuery は /api/logs の絞り込み
type LogQuery struct {
	Query string
	Level string
	From  time.Time
	To    time.Time
	Limit int
}

// ScopeJSON は 1 つのスコープに属する G の様子
type ScopeJSON struct {
	Name       string
	Goroutines int
	Waiting    int
	Leaked     int
}
