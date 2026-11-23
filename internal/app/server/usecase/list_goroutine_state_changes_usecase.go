package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type ListGoroutineStateChangesUsecaseInput struct {
	Service   observation.ServiceName
	Instance  observation.InstanceName
	Goroutine observation.GoroutineID
	Period    PeriodQuery
	Cursor    domain.PageCursor
	MaxBytes  int
	Now       time.Time
}

// ListGoroutineStateChangesUsecaseOutput の Found が false なら、期間の execution trace が残っていない
type ListGoroutineStateChangesUsecaseOutput struct {
	Period             domain.Period
	Found              bool
	TraceFromPreserved bool
	Changes            []domain.GoroutineStateChange
	Next               domain.PageCursor
	Unavailable        []Unavailable
	NearbyTraces       []domain.PreservedTrace
}

type ListGoroutineStateChangesUsecase struct {
	retention domain.Retention
	inventory domain.InventoryAccessor
	period    periodDeps
	source    traceSource
}

func (us *UseCase) NewListGoroutineStateChangesUsecase() *ListGoroutineStateChangesUsecase {
	src := traceSource{traces: us.traces, preserved: us.preserved}
	return &ListGoroutineStateChangesUsecase{retention: us.settings.Retention, inventory: us.inventory, period: periodDeps{logs: us.logs, issues: us.issues, traces: src}, source: src}
}

func (u *ListGoroutineStateChangesUsecase) Exec(ctx context.Context, in ListGoroutineStateChangesUsecaseInput) (ListGoroutineStateChangesUsecaseOutput, error) {
	if err := requireInstance(ctx, u.inventory, in.Service, in.Instance); err != nil {
		return ListGoroutineStateChangesUsecaseOutput{}, err
	}
	after, err := domain.ParsePageCursor(in.Cursor)
	if err != nil {
		return ListGoroutineStateChangesUsecaseOutput{}, err
	}
	p, err := u.period.decidePeriod(ctx, in.Service, in.Instance, in.Goroutine, in.Period, in.Now)
	if err != nil {
		return ListGoroutineStateChangesUsecaseOutput{}, err
	}
	out := ListGoroutineStateChangesUsecaseOutput{Period: p, Changes: []domain.GoroutineStateChange{}}
	joined, preserved, ok, err := u.source.trace(ctx, in.Service, in.Instance, p.From, p.To)
	if err != nil {
		return ListGoroutineStateChangesUsecaseOutput{}, err
	}
	if !ok {
		since := u.retention.RetainedSince(domain.DataExecutionTraces, in.Now)
		reason := ReasonNotRecorded
		if p.From.Before(since) {
			reason = ReasonPastRetention
		}
		out.Unavailable = []Unavailable{{Field: "stateChanges", Reason: reason, RetainedSince: since}}
		out.NearbyTraces, err = u.source.nearbyTraces(ctx, in.Service, in.Instance, p.From)
		if err != nil {
			return ListGoroutineStateChangesUsecaseOutput{}, err
		}
		return out, nil
	}
	out.Found, out.TraceFromPreserved = true, preserved
	changes := domain.StateChangesOf(joined, in.Goroutine)
	out.Changes, out.Next = domain.Page(changes, after, in.MaxBytes, func(c domain.GoroutineStateChange) int {
		return len(c.Function) + len(c.Location) + len(c.Reason) + len(c.WokenByFunction) + len(c.WokenByLocation) + 96
	})
	if out.Changes == nil {
		out.Changes = []domain.GoroutineStateChange{}
	}
	return out, nil
}
