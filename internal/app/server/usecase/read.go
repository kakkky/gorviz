package usecase

import (
	"context"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const candidateLimit = 10

func requireService(ctx context.Context, inventory domain.InventoryAccessor, s observation.ServiceName) error {
	services, err := inventory.ListServices(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(services, s) {
		return nil
	}
	names := make([]string, 0, len(services))
	for _, x := range services {
		names = append(names, string(x))
	}
	return domain.ErrNotFound.WithCandidates("no service named "+string(s), domain.Candidates(string(s), names, candidateLimit))
}

func requireInstance(ctx context.Context, inventory domain.InventoryAccessor, s observation.ServiceName, i observation.InstanceName) error {
	if err := requireService(ctx, inventory, s); err != nil {
		return err
	}
	instances, err := inventory.ListInstances(ctx, s)
	if err != nil {
		return err
	}
	if slices.Contains(instances, i) {
		return nil
	}
	names := make([]string, 0, len(instances))
	for _, x := range instances {
		names = append(names, string(x))
	}
	return domain.ErrNotFound.WithCandidates("no instance named "+string(i)+" in "+string(s), domain.Candidates(string(i), names, candidateLimit))
}

// findLog は ID のログ 1 行。見つからなければ、同じ時刻の近くのログを候補に添える
func findLog(ctx context.Context, logs domain.LogAccessor, s observation.ServiceName, id domain.LogID) (domain.StoredLog, error) {
	p, err := domain.ParseLogID(id)
	if err != nil {
		return domain.StoredLog{}, err
	}
	near, err := logs.ListLogs(ctx, s, p.At.Add(-time.Second), p.At.Add(time.Second))
	if err != nil {
		return domain.StoredLog{}, err
	}
	var candidates []string
	for _, l := range near {
		if l.ID() == id {
			return l, nil
		}
		candidates = append(candidates, string(l.ID()))
	}
	return domain.StoredLog{}, domain.ErrNotFound.WithCandidates("no log "+string(id), candidates[:min(len(candidates), candidateLimit)])
}

// PeriodQuery は goroutine の調べる期間を決める入口。From・To か、Log か、Issue と Occurrence か、Woken (と From・To) のどれか
type PeriodQuery struct {
	From       time.Time
	To         time.Time
	Log        domain.LogID
	Issue      string
	Occurrence time.Time
	Woken      observation.GoroutineID
}

type periodDeps struct {
	logs   domain.LogAccessor
	issues domain.IssueAccessor
	traces traceSource
}

// decidePeriod は入口から調べる期間を決める。入口がなければ直前の 1 分にする
func (d periodDeps) decidePeriod(ctx context.Context, s observation.ServiceName, i observation.InstanceName, g observation.GoroutineID, q PeriodQuery, now time.Time) (domain.Period, error) {
	switch {
	case q.Log != "":
		l, err := findLog(ctx, d.logs, s, q.Log)
		if err != nil {
			return domain.Period{}, err
		}
		from, to := domain.LogPeriod(l.LogLine)
		return domain.Period{From: from, To: to, By: domain.PeriodLog}, nil
	case q.Issue != "":
		o, err := findOccurrence(ctx, d.issues, s, q.Issue, i, q.Occurrence, now)
		if err != nil {
			return domain.Period{}, err
		}
		from, to := o.Around()
		return domain.Period{From: from, To: to, By: domain.PeriodOccurrence}, nil
	case q.Woken != 0:
		from, to := q.From, q.To
		if from.IsZero() || to.IsZero() {
			from, to = now.Add(-5*time.Minute), now
		}
		joined, _, ok, err := d.traces.trace(ctx, s, i, from, to)
		if err != nil {
			return domain.Period{}, err
		}
		if ok {
			if f, t, ok := domain.WokenPeriod(joined, q.Woken, g); ok {
				return domain.Period{From: f, To: t, By: domain.PeriodWoken}, nil
			}
		}
		return domain.Period{}, domain.ErrNotFound.With("no wait of the woken goroutine was ended by this goroutine in the execution trace of the period")
	case !q.From.IsZero() || !q.To.IsZero():
		from, to := q.From, q.To
		if to.IsZero() {
			to = now
		}
		if from.IsZero() {
			from = to.Add(-time.Minute)
		}
		return domain.NewPeriod(from, to, domain.PeriodGiven)
	}
	return domain.Period{From: now.Add(-time.Minute), To: now, By: domain.PeriodGiven}, nil
}

// findOccurrence は Issue の発生のうち、インスタンスと始まりが合うもの。始まりを渡さなければ、そのインスタンスの最後の発生
func findOccurrence(ctx context.Context, issues domain.IssueAccessor, s observation.ServiceName, name string, i observation.InstanceName, at, now time.Time) (domain.Occurrence, error) {
	from, to := at, at
	if at.IsZero() {
		to = now
		from = to.Add(-31 * 24 * time.Hour)
	}
	days, err := issues.ListIssues(ctx, s, from, to)
	if err != nil {
		return domain.Occurrence{}, err
	}
	var (
		found domain.Occurrence
		ok    bool
	)
	for _, d := range days {
		if d.Issue.Name != name {
			continue
		}
		for _, o := range d.Issue.Occurrences {
			if (i == "" || o.Instance == i) && (at.IsZero() || o.StartedAt.Equal(at)) && (!ok || o.StartedAt.After(found.StartedAt)) {
				found, ok = o, true
			}
		}
	}
	if !ok {
		return domain.Occurrence{}, domain.ErrNotFound.With("no occurrence of the issue " + name + " at the instance and time")
	}
	return found, nil
}

// traceSource は期間の execution trace を、本体か、残っていなければ Issue の発生・エラーログの前後に残したものから引く
type traceSource struct {
	traces    domain.TraceAccessor
	preserved domain.PreservedTraceAccessor
}

func (t traceSource) trace(ctx context.Context, s observation.ServiceName, i observation.InstanceName, from, to time.Time) (observation.TraceChunk, bool, bool, error) {
	chunks, err := t.traces.ListTraceChunks(ctx, s, i, from, to)
	if err != nil {
		return observation.TraceChunk{}, false, false, err
	}
	if joined, ok := observation.Join(chunks, from, to); ok {
		return joined, false, true, nil
	}
	if t.preserved == nil {
		return observation.TraceChunk{}, false, false, nil
	}
	ps, err := t.preserved.ListPreservedTracesBetween(ctx, s, i, from, to)
	if err != nil {
		return observation.TraceChunk{}, false, false, err
	}
	var parts []observation.TraceChunk
	for _, p := range ps {
		if !p.From.Before(to) || !p.To.After(from) {
			continue
		}
		c, ok, err := t.preserved.ReadPreservedTrace(ctx, s, i, p)
		if err != nil {
			return observation.TraceChunk{}, false, false, err
		}
		if ok {
			parts = append(parts, c)
		}
	}
	slices.SortFunc(parts, func(a, b observation.TraceChunk) int { return a.StartedAt.Compare(b.StartedAt) })
	joined, ok := observation.Join(parts, from, to)
	return joined, true, ok, nil
}

// nearbyTraces は at の前後 1 日に残した execution trace を近い順に
func (t traceSource) nearbyTraces(ctx context.Context, s observation.ServiceName, i observation.InstanceName, at time.Time) ([]domain.PreservedTrace, error) {
	ps, err := t.preserved.ListPreservedTracesBetween(ctx, s, i, at.Add(-24*time.Hour), at.Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	return domain.Nearby(ps, at, 5), nil
}
