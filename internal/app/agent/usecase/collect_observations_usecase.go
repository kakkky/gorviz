package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type CollectObservationsUsecase struct {
	targets   domain.TargetRegistry
	states    domain.TargetStateStore
	memory    domain.GoroutineMemoryStore
	lifecycle domain.InstanceLifecycle
	buffer    domain.ObservationBuffer
	runtime   domain.RuntimeClient
}

func (us *UseCase) NewCollectObservationsUsecase() *CollectObservationsUsecase {
	return &CollectObservationsUsecase{targets: us.targets, states: us.states, memory: us.memory, lifecycle: us.lifecycle, buffer: us.buffer, runtime: us.runtime}
}

type CollectObservationsUsecaseInput struct {
	Mode              domain.Mode
	ChunkLength       time.Duration
	LeakCheckInterval time.Duration
}

// CollectObservationsUsecaseOutput の Collected はチャンクを積めたインスタンスの数
type CollectObservationsUsecaseOutput struct {
	Collected int
}

// Exec はチャンク 1 つ分を、観測するアプリごとに並べて取る。1 つのアプリで失敗しても、ほかのアプリは続ける
func (u *CollectObservationsUsecase) Exec(ctx context.Context, in CollectObservationsUsecaseInput) (CollectObservationsUsecaseOutput, error) {
	targets, err := u.targets.Targets(ctx)
	if err != nil {
		return CollectObservationsUsecaseOutput{}, err
	}
	results := make([]bool, len(targets))
	err = scope.Run(ctx, func(sc *scope.Scope) error {
		for i, t := range targets {
			sc.Go(func(ctx context.Context) error {
				ok, err := u.collect(ctx, in, t)
				results[i] = ok
				return err
			})
		}
		return nil
	}, scope.WithSupervisor(), scope.WithErrAggregation())
	var out CollectObservationsUsecaseOutput
	for _, ok := range results {
		if ok {
			out.Collected++
		}
	}
	if ctx.Err() != nil {
		return out, nil
	}
	return out, err
}

func (u *CollectObservationsUsecase) collect(ctx context.Context, in CollectObservationsUsecaseInput, t domain.TargetInstance) (bool, error) {
	saved, err := u.states.TargetState(ctx, t.ID)
	if err != nil {
		return false, err
	}
	state := saved.For(t.Instance)
	raw, err := u.runtime.FetchExecutionTrace(ctx, t, in.ChunkLength)
	if err != nil {
		return false, err
	}
	parsed, err := domain.ParseExecutionTrace(raw.Data, raw.ReceivedAt, state.LastTraceEnd)
	if err != nil {
		return false, err
	}
	var errs []error
	if profile, err := u.runtime.FetchThreadCreateProfile(ctx, t); err != nil {
		errs = append(errs, err)
	} else if parsed.Chunk.Threads, err = domain.ParseThreadCount(profile); err != nil {
		errs = append(errs, err)
	}

	instance := t.Instance
	if domain.ProcessReplaced(in.Mode, state.MaxAliveGoroutineID, parsed.MaxGoroutineID) {
		instance.Incarnation = domain.NewIncarnation(raw.ReceivedAt)
		if err := u.targets.UpdateTargets(ctx, func(current []domain.TargetInstance) []domain.TargetInstance {
			return domain.ReplaceInstance(current, t.ID, instance)
		}); err != nil {
			return false, err
		}
		if err := u.lifecycle.MarkEnded(ctx, t.Instance); err != nil {
			return false, err
		}
		state = domain.TargetState{Instance: instance}
	}

	memory, err := u.memory.GoroutineMemory(ctx, instance)
	if err != nil {
		return false, err
	}
	domain.InheritWaitReasons(&parsed.Chunk, memory)
	snapshot, memory := domain.BuildSnapshot(parsed, memory)
	if err := u.memory.SetGoroutineMemory(ctx, instance, memory); err != nil {
		return false, err
	}
	if err := u.buffer.PushTraceChunk(ctx, instance, parsed.Chunk); err != nil {
		return false, err
	}
	if err := u.buffer.PushSnapshot(ctx, instance, snapshot); err != nil {
		return false, err
	}
	state.MaxAliveGoroutineID = domain.MaxAliveGoroutineID(parsed)
	state.LastTraceEnd = parsed.Chunk.EndedAt()

	if err := u.checkLeaks(ctx, in, t, instance, &state, raw.ReceivedAt); err != nil {
		errs = append(errs, err)
	}
	if err := u.states.SetTargetState(ctx, t.ID, state); err != nil {
		return true, err
	}
	return true, errors.Join(errs...)
}

func (u *CollectObservationsUsecase) checkLeaks(ctx context.Context, in CollectObservationsUsecaseInput, t domain.TargetInstance, instance observation.Instance, state *domain.TargetState, now time.Time) error {
	var check observation.GoroutineLeakCheck
	switch domain.PlanLeakCheck(state.LastLeakCheck, now, in.LeakCheckInterval) {
	case domain.LeakCheckSkip:
		return nil
	case domain.LeakCheckReportDisabled:
		check = domain.DisabledLeakCheck(now)
	case domain.LeakCheckFetch:
		profile, err := u.runtime.FetchGoroutineLeakProfile(ctx, t)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			check = domain.UnsupportedLeakCheck(now)
		case err != nil:
			return err
		default:
			if check, err = domain.ParseGoroutineLeakProfile(profile, now); err != nil {
				return err
			}
		}
	}
	if err := u.buffer.PushLeakCheck(ctx, instance, check); err != nil {
		return err
	}
	state.LastLeakCheck = now
	return nil
}
