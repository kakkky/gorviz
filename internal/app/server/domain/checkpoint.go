package domain

import (
	"cmp"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// Process は観測データから作る処理。チェックポイントは処理ごとに持つ
type Process string

const (
	ProcessAggregateMetrics                   Process = "aggregate-metrics"
	ProcessAggregateGoroutineStateBreakdowns  Process = "aggregate-goroutine-state-breakdowns"
	ProcessDetectIssues                       Process = "detect-issues"
	ProcessDetectIssuesBaseline               Process = "detect-issues-baseline"
	ProcessPreserveExecutionTracesAroundError Process = "preserve-execution-traces-around-error-logs"
	ProcessAttachLogWriterGoroutines          Process = "attach-log-writer-goroutines"
)

// ServiceKey はサービスに 1 つのチェックポイント (普段との比較) のキー
const ServiceKey = ""

// Checkpoint はどこまで処理したか。At は最後に処理した観測 (区切りなら区切りの始まり) の時刻。
// ログは同じ時刻が並ぶので、番号まで持つ。Open は追っている発生 (Issue の検出だけ)
type Checkpoint struct {
	At       time.Time           `json:"at"`
	LogAgent observation.AgentID `json:"logAgent,omitempty"`
	LogSeq   uint64              `json:"logSeq,omitempty"`
	Open     []TrackedOccurrence `json:"open,omitempty"`
}

// AfterLog はログ l がチェックポイントより後か。ログの順は時刻・agent・番号
func (c Checkpoint) AfterLog(l observation.LogLine) bool {
	return cmp.Or(l.At.Compare(c.At), cmp.Compare(l.Agent, c.LogAgent), cmp.Compare(l.Seq, c.LogSeq)) > 0
}

func CompareLogs(a, b observation.LogLine) int {
	return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.Agent, b.Agent), cmp.Compare(a.Seq, b.Seq))
}

// Checkpoints はサービスの、処理ごと・インスタンスごとのチェックポイント
type Checkpoints map[Process]map[string]Checkpoint

func (c Checkpoints) Get(p Process, key string) Checkpoint {
	return c[p][key]
}

// Prune は指している観測データが保持期間ですべて消えたチェックポイントと、もうないインスタンスのものを捨てる
func (c Checkpoints) Prune(instances map[observation.InstanceName]bool, r Retention, now time.Time) Checkpoints {
	out := Checkpoints{}
	for p, byKey := range c {
		for k, cp := range byKey {
			if k != ServiceKey && !instances[observation.InstanceName(k)] {
				continue
			}
			if r.Expired(processInput(p), processInput(p).PeriodStart(cp.At), now) && len(cp.Open) == 0 {
				continue
			}
			if out[p] == nil {
				out[p] = map[string]Checkpoint{}
			}
			out[p][k] = cp
		}
	}
	return out
}

func processInput(p Process) DataKind {
	switch p {
	case ProcessAggregateMetrics, ProcessAggregateGoroutineStateBreakdowns, ProcessDetectIssues:
		return DataExecutionTraces
	case ProcessDetectIssuesBaseline:
		return DataMetrics
	}
	return DataLogs
}
