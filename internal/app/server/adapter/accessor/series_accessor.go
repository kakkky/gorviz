package accessor

import (
	"context"
	"path"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// listLines は [from, to] に重なる時間帯のファイルを読み、at が [from, to] に入るものを返す
func listLines[T any](root *filesystem.Root, k domain.DataKind, from, to time.Time, fileOf func(time.Time) string, at func(T) time.Time) ([]T, error) {
	var out []T
	for _, p := range periods(k, from, to, 0) {
		err := eachLine(root, fileOf(p), func(v T) {
			if t := at(v); !t.Before(from) && !t.After(to) {
				out = append(out, v)
			}
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

type LifecycleAccessor struct{ root *filesystem.Root }

func NewLifecycleAccessor(root *filesystem.Root) *LifecycleAccessor {
	return &LifecycleAccessor{root: root}
}

func (a *LifecycleAccessor) AppendLifecycle(ctx context.Context, s observation.ServiceName, events []domain.LifecycleEvent) error {
	if err := appendLines(a.root, events, func(e domain.LifecycleEvent) string {
		return serviceFile(s, domain.DataInstanceLifecycle, e.At, ".jsonl")
	}); err != nil {
		return domain.ErrInternal.Wrap(err, "append instance lifecycle")
	}
	return nil
}

func (a *LifecycleAccessor) ListLifecycle(ctx context.Context, s observation.ServiceName, from, to time.Time) ([]domain.LifecycleEvent, error) {
	out, err := listLines(a.root, domain.DataInstanceLifecycle, from, to, func(t time.Time) string {
		return serviceFile(s, domain.DataInstanceLifecycle, t, ".jsonl")
	}, func(e domain.LifecycleEvent) time.Time { return e.At })
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read instance lifecycle")
	}
	return out, nil
}

func (a *LifecycleAccessor) LastLifecycle(ctx context.Context, s observation.ServiceName, i observation.InstanceName) (domain.LifecycleEvent, bool, error) {
	dir := path.Join(serviceDir(s), string(domain.DataInstanceLifecycle))
	entries, err := a.root.ReadDir(dir)
	if err != nil {
		return domain.LifecycleEvent{}, false, domain.ErrInternal.Wrap(err, "list instance lifecycle")
	}
	for j := len(entries) - 1; j >= 0; j-- {
		events, err := readLines[domain.LifecycleEvent](a.root, path.Join(dir, entries[j].Name()))
		if err != nil {
			return domain.LifecycleEvent{}, false, domain.ErrInternal.Wrap(err, "read instance lifecycle")
		}
		for k := len(events) - 1; k >= 0; k-- {
			if events[k].Instance == i {
				return events[k], true, nil
			}
		}
	}
	return domain.LifecycleEvent{}, false, nil
}

type MetricAccessor struct{ root *filesystem.Root }

func NewMetricAccessor(root *filesystem.Root) *MetricAccessor { return &MetricAccessor{root: root} }

func (a *MetricAccessor) AppendMetrics(ctx context.Context, s observation.ServiceName, samples []domain.MetricSample) error {
	if err := appendLines(a.root, samples, func(m domain.MetricSample) string {
		return serviceFile(s, domain.DataMetrics, m.At, ".jsonl")
	}); err != nil {
		return domain.ErrInternal.Wrap(err, "append metrics")
	}
	return nil
}

func (a *MetricAccessor) ListMetrics(ctx context.Context, s observation.ServiceName, from, to time.Time) ([]domain.MetricSample, error) {
	out, err := listLines(a.root, domain.DataMetrics, from, to, func(t time.Time) string {
		return serviceFile(s, domain.DataMetrics, t, ".jsonl")
	}, func(m domain.MetricSample) time.Time { return m.At })
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read metrics")
	}
	return dedupe(out, func(m domain.MetricSample) string { return m.At.String() + "\x00" + string(m.Instance) }), nil
}

type BreakdownAccessor struct{ root *filesystem.Root }

func NewBreakdownAccessor(root *filesystem.Root) *BreakdownAccessor {
	return &BreakdownAccessor{root: root}
}

func (a *BreakdownAccessor) AppendBreakdowns(ctx context.Context, s observation.ServiceName, i observation.InstanceName, rows []domain.MinuteBreakdown) error {
	if err := appendLines(a.root, rows, func(r domain.MinuteBreakdown) string {
		return instanceFile(s, i, domain.DataGoroutineStateBreakdowns, r.Minute, ".jsonl")
	}); err != nil {
		return domain.ErrInternal.Wrap(err, "append goroutine state breakdowns")
	}
	return nil
}

func (a *BreakdownAccessor) ListBreakdowns(ctx context.Context, s observation.ServiceName, i observation.InstanceName, from, to time.Time) ([]domain.MinuteBreakdown, error) {
	out, err := listLines(a.root, domain.DataGoroutineStateBreakdowns, from, to, func(t time.Time) string {
		return instanceFile(s, i, domain.DataGoroutineStateBreakdowns, t, ".jsonl")
	}, func(r domain.MinuteBreakdown) time.Time { return r.Minute })
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read goroutine state breakdowns")
	}
	return dedupe(out, func(r domain.MinuteBreakdown) string {
		return r.Minute.String() + "\x00" + strconv.FormatInt(int64(r.Goroutine), 10)
	}), nil
}

type LogWriterAccessor struct{ root *filesystem.Root }

func NewLogWriterAccessor(root *filesystem.Root) *LogWriterAccessor {
	return &LogWriterAccessor{root: root}
}

func (a *LogWriterAccessor) AppendLogWriters(ctx context.Context, s observation.ServiceName, i observation.InstanceName, writers []domain.LogWriter) error {
	if err := appendLines(a.root, writers, func(w domain.LogWriter) string {
		return instanceFile(s, i, domain.DataLogWriterGoroutines, w.At, ".jsonl")
	}); err != nil {
		return domain.ErrInternal.Wrap(err, "append log writer goroutines")
	}
	return nil
}

func (a *LogWriterAccessor) ListLogWriters(ctx context.Context, s observation.ServiceName, i observation.InstanceName, from, to time.Time) ([]domain.LogWriter, error) {
	out, err := listLines(a.root, domain.DataLogWriterGoroutines, from, to, func(t time.Time) string {
		return instanceFile(s, i, domain.DataLogWriterGoroutines, t, ".jsonl")
	}, func(w domain.LogWriter) time.Time { return w.At })
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read log writer goroutines")
	}
	return dedupe(out, func(w domain.LogWriter) string { return string(w.Log) }), nil
}

// dedupe はチェックポイントを進める前に落ちて同じ分を 2 回書いたものを、後の 1 つにまとめる
func dedupe[T any](items []T, key func(T) string) []T {
	last := map[string]int{}
	for i, it := range items {
		last[key(it)] = i
	}
	out := items[:0:0]
	for i, it := range items {
		if last[key(it)] == i {
			out = append(out, it)
		}
	}
	return out
}
