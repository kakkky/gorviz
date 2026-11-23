package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type SummarizeGoroutinesUsecaseInput struct {
	Service  observation.ServiceName
	Instance observation.InstanceName
	From     time.Time
	To       time.Time
	Cursor   domain.PageCursor
	MaxBytes int
	Now      time.Time
}

// SummarizeGoroutinesUsecaseOutput の Source は算出に使ったデータ。スナップショットからのときは時間を持たないので、Breakdown は状態ごとの待ち時間の合計になる
type SummarizeGoroutinesUsecaseOutput struct {
	Period        domain.Period
	Source        domain.DiagnosisSource
	ComputedFrom  domain.Period
	MinuteAligned bool
	Summaries     []domain.CreatorSummary
	Next          domain.PageCursor
	Unavailable   []Unavailable
}

type SummarizeGoroutinesUsecase struct {
	retention  domain.Retention
	inventory  domain.InventoryAccessor
	source     traceSource
	breakdowns domain.BreakdownAccessor
	snapshots  domain.SnapshotAccessor
}

func (us *UseCase) NewSummarizeGoroutinesUsecase() *SummarizeGoroutinesUsecase {
	return &SummarizeGoroutinesUsecase{retention: us.settings.Retention, inventory: us.inventory, source: traceSource{traces: us.traces, preserved: us.preserved},
		breakdowns: us.breakdowns, snapshots: us.snapshots}
}

func (u *SummarizeGoroutinesUsecase) Exec(ctx context.Context, in SummarizeGoroutinesUsecaseInput) (SummarizeGoroutinesUsecaseOutput, error) {
	if err := requireInstance(ctx, u.inventory, in.Service, in.Instance); err != nil {
		return SummarizeGoroutinesUsecaseOutput{}, err
	}
	after, err := domain.ParsePageCursor(in.Cursor)
	if err != nil {
		return SummarizeGoroutinesUsecaseOutput{}, err
	}
	from, to := in.From, in.To
	if to.IsZero() {
		to = in.Now
	}
	if from.IsZero() {
		from = to.Add(-time.Minute)
	}
	p, err := domain.NewPeriod(from, to, domain.PeriodGiven)
	if err != nil {
		return SummarizeGoroutinesUsecaseOutput{}, err
	}
	s, i := in.Service, in.Instance
	snapshots, err := u.snapshots.ListSnapshots(ctx, s, i, from.Add(-time.Minute), to.Add(time.Minute))
	if err != nil {
		return SummarizeGoroutinesUsecaseOutput{}, err
	}
	creators := domain.Creators(snapshots)
	out := SummarizeGoroutinesUsecaseOutput{Period: p}
	var all []domain.CreatorSummary
	joined, _, ok, err := u.source.trace(ctx, s, i, from, to)
	if err != nil {
		return SummarizeGoroutinesUsecaseOutput{}, err
	}
	switch {
	case ok:
		out.Source, out.ComputedFrom = domain.SourceExecutionTrace, p
		all = domain.SummarizeTrace(joined, creators, from, to)
	default:
		out.Unavailable = append(out.Unavailable, unavailableOf(u.retention, "executionTrace", domain.DataExecutionTraces, from, in.Now))
		rows, err := u.breakdowns.ListBreakdowns(ctx, s, i, from.Truncate(domain.BreakdownStep), to)
		if err != nil {
			return SummarizeGoroutinesUsecaseOutput{}, err
		}
		if len(rows) > 0 {
			aFrom := from.Truncate(domain.BreakdownStep)
			aTo := to.Truncate(domain.BreakdownStep)
			if aTo.Before(to) {
				aTo = aTo.Add(domain.BreakdownStep)
			}
			out.Source, out.ComputedFrom, out.MinuteAligned = domain.SourceStateBreakdowns, domain.Period{From: aFrom, To: aTo}, !aFrom.Equal(from) || !aTo.Equal(to)
			all = domain.SummarizeBreakdowns(rows, creators)
		} else if snap, ok := domain.NearestSnapshot(snapshots, to); ok {
			out.Unavailable = append(out.Unavailable, unavailableOf(u.retention, "breakdown", domain.DataGoroutineStateBreakdowns, from, in.Now))
			out.Source, out.ComputedFrom = domain.SourceSnapshots, domain.Period{From: snap.At, To: snap.At}
			all = domain.SummarizeSnapshot(snap)
		}
	}
	out.Summaries, out.Next = domain.Page(all, after, in.MaxBytes, func(c domain.CreatorSummary) int {
		return len(c.Creator) + 64 + 32*len(c.Breakdown.Durations) + 8*len(c.Sample)
	})
	if out.Summaries == nil {
		out.Summaries = []domain.CreatorSummary{}
	}
	return out, nil
}

func unavailableOf(r domain.Retention, field string, k domain.DataKind, at, now time.Time) Unavailable {
	since := r.RetainedSince(k, now)
	if at.Before(since) {
		return Unavailable{Field: field, Reason: ReasonPastRetention, RetainedSince: since}
	}
	return Unavailable{Field: field, Reason: ReasonNotRecorded, RetainedSince: since}
}
