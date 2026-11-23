package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type IngestObservationsUsecaseInput struct {
	Batches []observation.Batch
	Now     time.Time
}

// IngestObservationsUsecaseOutput の Cursors は、インスタンスごとにディスクに書いてある位置
type IngestObservationsUsecaseOutput struct {
	Cursors map[observation.Instance]observation.Cursor
}

type IngestObservationsUsecase struct {
	retention  domain.Retention
	locker     domain.InstanceLocker
	holdings   domain.HoldingAccessor
	lifecycle  domain.LifecycleAccessor
	logs       domain.LogAccessor
	traces     domain.TraceAccessor
	snapshots  domain.SnapshotAccessor
	leakChecks domain.LeakCheckAccessor
}

func (us *UseCase) NewIngestObservationsUsecase() *IngestObservationsUsecase {
	return &IngestObservationsUsecase{
		retention: us.settings.Retention, locker: us.locker, holdings: us.holdings, lifecycle: us.lifecycle,
		logs: us.logs, traces: us.traces, snapshots: us.snapshots, leakChecks: us.leakChecks,
	}
}

func (u *IngestObservationsUsecase) Exec(ctx context.Context, in IngestObservationsUsecaseInput) (IngestObservationsUsecaseOutput, error) {
	for _, b := range in.Batches {
		if b.Instance.Service == "" || b.Instance.Name == "" {
			return IngestObservationsUsecaseOutput{}, domain.ErrInvalidArgument.With("a batch needs the service and the instance name")
		}
	}
	out := IngestObservationsUsecaseOutput{Cursors: map[observation.Instance]observation.Cursor{}}
	for _, b := range in.Batches {
		c, err := u.ingest(ctx, b, in.Now)
		if err != nil {
			return IngestObservationsUsecaseOutput{}, err
		}
		out.Cursors[b.Instance] = c
	}
	return out, nil
}

func (u *IngestObservationsUsecase) ingest(ctx context.Context, b observation.Batch, now time.Time) (observation.Cursor, error) {
	s, i := b.Instance.Service, b.Instance.Name
	unlock := u.locker.LockInstance(s, i)
	defer unlock()
	held, err := u.holdings.Holding(ctx, s, i)
	if err != nil {
		return observation.Cursor{}, err
	}
	last, hasLast, err := u.lifecycle.LastLifecycle(ctx, s, i)
	if err != nil {
		return observation.Cursor{}, err
	}
	if events := domain.LifecycleChanges(last, hasLast, b, now); len(events) > 0 {
		if err := u.lifecycle.AppendLifecycle(ctx, s, events); err != nil {
			return observation.Cursor{}, err
		}
	}
	plan := domain.PlanIngest(b, held.Cursor, u.retention, now)
	if plan.Empty() {
		return held.Cursor, nil
	}
	if err := u.write(ctx, b.Instance, plan); err != nil {
		u.holdings.ForgetHolding(ctx, s, i)
		return observation.Cursor{}, err
	}
	next := plan.AdvanceHolding(held)
	u.holdings.RememberHolding(ctx, s, i, next)
	return next.Cursor, nil
}

func (u *IngestObservationsUsecase) write(ctx context.Context, inst observation.Instance, plan domain.IngestPlan) error {
	if len(plan.Logs) > 0 {
		if err := u.logs.AppendLogs(ctx, inst, plan.Logs); err != nil {
			return err
		}
	}
	if len(plan.TraceChunks) > 0 {
		if err := u.traces.AppendTraceChunks(ctx, inst, plan.TraceChunks); err != nil {
			return err
		}
	}
	if len(plan.Snapshots) > 0 {
		if err := u.snapshots.AppendSnapshots(ctx, inst, plan.Snapshots); err != nil {
			return err
		}
	}
	if len(plan.LeakChecks) > 0 {
		if err := u.leakChecks.AppendLeakChecks(ctx, inst, plan.LeakChecks); err != nil {
			return err
		}
	}
	return nil
}
