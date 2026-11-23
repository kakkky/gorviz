package domain

import (
	"cmp"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// DataKind は保存先のデータの種類。名前はディレクトリの名前と同じ
type DataKind string

const (
	DataMetrics                        DataKind = "metrics"
	DataIssues                         DataKind = "issues"
	DataLogs                           DataKind = "logs"
	DataInstanceLifecycle              DataKind = "instance-lifecycle"
	DataExecutionTraces                DataKind = "execution-traces"
	DataExecutionTracesAroundIssues    DataKind = "execution-traces-around-issues"
	DataExecutionTracesAroundErrorLogs DataKind = "execution-traces-around-error-logs"
	DataGoroutineSnapshots             DataKind = "goroutine-snapshots"
	DataGoroutineLeakChecks            DataKind = "goroutine-leak-checks"
	DataGoroutineStateBreakdowns       DataKind = "goroutine-state-breakdowns"
	DataLogWriterGoroutines            DataKind = "log-writer-goroutines"
)

var DataKinds = []DataKind{
	DataMetrics, DataIssues, DataLogs, DataInstanceLifecycle, DataExecutionTraces, DataExecutionTracesAroundIssues,
	DataExecutionTracesAroundErrorLogs, DataGoroutineSnapshots, DataGoroutineLeakChecks, DataGoroutineStateBreakdowns, DataLogWriterGoroutines,
}

// Period はファイル (日ごとのディレクトリ) 1 つが持つ時間帯の長さ
func (k DataKind) Period() time.Duration {
	switch k {
	case DataExecutionTraces, DataGoroutineSnapshots, DataLogs, DataLogWriterGoroutines:
		return time.Hour
	}
	return 24 * time.Hour
}

// PeriodStart は t を含むファイルの時間帯の始まり (UTC)
func (k DataKind) PeriodStart(t time.Time) time.Time {
	return t.UTC().Truncate(k.Period())
}

type Retention struct {
	Metrics                        time.Duration
	Issues                         time.Duration
	Logs                           time.Duration
	LogWriterGoroutines            time.Duration
	InstanceLifecycle              time.Duration
	ExecutionTraces                time.Duration
	ExecutionTracesAroundIssues    time.Duration
	ExecutionTracesAroundErrorLogs time.Duration
	GoroutineSnapshots             time.Duration
	GoroutineLeakChecks            time.Duration
	GoroutineStateBreakdowns       time.Duration
}

func (r Retention) For(k DataKind) time.Duration {
	switch k {
	case DataMetrics:
		return r.Metrics
	case DataIssues:
		return r.Issues
	case DataLogs:
		return r.Logs
	case DataLogWriterGoroutines:
		return r.LogWriterGoroutines
	case DataInstanceLifecycle:
		return r.InstanceLifecycle
	case DataExecutionTraces:
		return r.ExecutionTraces
	case DataExecutionTracesAroundIssues:
		return r.ExecutionTracesAroundIssues
	case DataExecutionTracesAroundErrorLogs:
		return r.ExecutionTracesAroundErrorLogs
	case DataGoroutineSnapshots:
		return r.GoroutineSnapshots
	case DataGoroutineLeakChecks:
		return r.GoroutineLeakChecks
	case DataGoroutineStateBreakdowns:
		return r.GoroutineStateBreakdowns
	}
	return 0
}

// Expired はファイルの時間帯の終わりが保持期間を過ぎたか。期間の中のデータを 1 つでも含むファイルは捨てない
func (r Retention) Expired(k DataKind, periodStart, now time.Time) bool {
	return !periodStart.Add(k.Period()).After(now.Add(-r.For(k)))
}

// Keeps は t のデータを書いてよいか。書いてもすぐ捨てるファイルを作り直さないため
func (r Retention) Keeps(k DataKind, t, now time.Time) bool {
	return !r.Expired(k, k.PeriodStart(t), now)
}

// RetainedSince は k のうちまだ残っている最も古い時刻。これより前は保持期間を過ぎて出せない
func (r Retention) RetainedSince(k DataKind, now time.Time) time.Time {
	start := k.PeriodStart(now.Add(-r.For(k)))
	if r.Expired(k, start, now) {
		start = start.Add(k.Period())
	}
	return start
}

// DataFile は保存先の、データの種類ごとの 1 ファイル (日ごとのディレクトリなら 1 ディレクトリ)
type DataFile struct {
	Kind        DataKind
	Service     observation.ServiceName
	Instance    observation.InstanceName
	Path        string
	PeriodStart time.Time
	Size        int64
}

// ExpiredFiles は保持期間を過ぎたファイル
func (r Retention) ExpiredFiles(files []DataFile, now time.Time) []DataFile {
	var out []DataFile
	for _, f := range files {
		if r.Expired(f.Kind, f.PeriodStart, now) {
			out = append(out, f)
		}
	}
	return out
}

// OverCapacity は容量の上限を下回るまで、データの種類ごとに一番古い日の分を 1 日ずつ選ぶ。
// 1 種類だけを削ると、その種類に頼る画面だけが使えなくなるので、すべての種類から捨てる。書いている途中の日 (today) は選ばない
func OverCapacity(files []DataFile, limit int64, now time.Time) []DataFile {
	if limit <= 0 {
		return nil
	}
	var usage int64
	for _, f := range files {
		usage += f.Size
	}
	today := now.UTC().Truncate(24 * time.Hour)
	byKind := map[DataKind]map[time.Time][]DataFile{}
	for _, f := range files {
		day := f.PeriodStart.UTC().Truncate(24 * time.Hour)
		if !day.Before(today) {
			continue
		}
		if byKind[f.Kind] == nil {
			byKind[f.Kind] = map[time.Time][]DataFile{}
		}
		byKind[f.Kind][day] = append(byKind[f.Kind][day], f)
	}
	var out []DataFile
	for usage > limit {
		dropped := false
		for _, k := range DataKinds {
			days := byKind[k]
			if len(days) == 0 {
				continue
			}
			oldest := slices.MinFunc(mapKeys(days), func(a, b time.Time) int { return a.Compare(b) })
			for _, f := range days[oldest] {
				usage -= f.Size
				out = append(out, f)
			}
			delete(days, oldest)
			dropped = true
		}
		if !dropped {
			break
		}
	}
	slices.SortFunc(out, func(a, b DataFile) int { return cmp.Compare(a.Path, b.Path) })
	return out
}

func mapKeys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
