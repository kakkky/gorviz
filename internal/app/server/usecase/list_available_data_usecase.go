package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// snapshotGap はスナップショットの並びを 1 つの範囲にまとめるときの、間の空きの上限
const snapshotGap = time.Minute

type ListAvailableDataUsecaseInput struct {
	Service  observation.ServiceName
	Instance observation.InstanceName
	From     time.Time
	To       time.Time
	Now      time.Time
}

// ListAvailableDataUsecaseOutput はある期間について、どのデータが残っているか。RetainedSince はデータごとの、まだ残っている最も古い時刻
type ListAvailableDataUsecaseOutput struct {
	From            time.Time
	To              time.Time
	ExecutionTraces []domain.TimeRange
	StateBreakdowns []domain.TimeRange
	Snapshots       []domain.TimeRange
	LeakChecks      []time.Time
	PreservedTraces []domain.PreservedTrace
	RetainedSince   map[domain.DataKind]time.Time
}

type ListAvailableDataUsecase struct {
	retention  domain.Retention
	inventory  domain.InventoryAccessor
	traces     domain.TraceAccessor
	breakdowns domain.BreakdownAccessor
	snapshots  domain.SnapshotAccessor
	leakChecks domain.LeakCheckAccessor
	preserved  domain.PreservedTraceAccessor
}

func (us *UseCase) NewListAvailableDataUsecase() *ListAvailableDataUsecase {
	return &ListAvailableDataUsecase{retention: us.settings.Retention, inventory: us.inventory, traces: us.traces, breakdowns: us.breakdowns,
		snapshots: us.snapshots, leakChecks: us.leakChecks, preserved: us.preserved}
}

func (u *ListAvailableDataUsecase) Exec(ctx context.Context, in ListAvailableDataUsecaseInput) (ListAvailableDataUsecaseOutput, error) {
	if err := requireInstance(ctx, u.inventory, in.Service, in.Instance); err != nil {
		return ListAvailableDataUsecaseOutput{}, err
	}
	from, to := in.From, in.To
	if to.IsZero() {
		to = in.Now
	}
	if from.IsZero() {
		from = to.Add(-time.Hour)
	}
	s, i := in.Service, in.Instance
	out := ListAvailableDataUsecaseOutput{From: from, To: to, RetainedSince: map[domain.DataKind]time.Time{}}
	for _, k := range []domain.DataKind{domain.DataExecutionTraces, domain.DataGoroutineStateBreakdowns, domain.DataGoroutineSnapshots, domain.DataGoroutineLeakChecks,
		domain.DataExecutionTracesAroundIssues, domain.DataExecutionTracesAroundErrorLogs} {
		out.RetainedSince[k] = u.retention.RetainedSince(k, in.Now)
	}
	var err error
	if out.ExecutionTraces, err = u.traces.ListTraceRanges(ctx, s, i, from, to); err != nil {
		return ListAvailableDataUsecaseOutput{}, err
	}
	rows, err := u.breakdowns.ListBreakdowns(ctx, s, i, from.Truncate(domain.BreakdownStep), to)
	if err != nil {
		return ListAvailableDataUsecaseOutput{}, err
	}
	var minutes []time.Time
	for _, r := range rows {
		if n := len(minutes); n == 0 || !minutes[n-1].Equal(r.Minute) {
			minutes = append(minutes, r.Minute)
		}
	}
	out.StateBreakdowns = ranges(minutes, domain.BreakdownStep, domain.BreakdownStep)
	snaps, err := u.snapshots.ListSnapshots(ctx, s, i, from, to)
	if err != nil {
		return ListAvailableDataUsecaseOutput{}, err
	}
	var snapTimes []time.Time
	for _, sn := range snaps {
		snapTimes = append(snapTimes, sn.At)
	}
	out.Snapshots = ranges(snapTimes, 0, snapshotGap)
	checks, err := u.leakChecks.ListLeakChecks(ctx, s, i, from, to)
	if err != nil {
		return ListAvailableDataUsecaseOutput{}, err
	}
	for _, c := range checks {
		out.LeakChecks = append(out.LeakChecks, c.At)
	}
	if out.PreservedTraces, err = u.preserved.ListPreservedTracesBetween(ctx, s, i, from, to); err != nil {
		return ListAvailableDataUsecaseOutput{}, err
	}
	return out, nil
}

// ranges は時刻順の点を、間が gap 以下なら 1 つの範囲につなぐ。width は 1 点が持つ長さ
func ranges(ts []time.Time, width, gap time.Duration) []domain.TimeRange {
	var out []domain.TimeRange
	for _, t := range ts {
		if n := len(out); n > 0 && !t.After(out[n-1].To.Add(gap)) {
			out[n-1].To = maxTime(out[n-1].To, t.Add(width))
			continue
		}
		out = append(out, domain.TimeRange{From: t, To: t.Add(width)})
	}
	return out
}
