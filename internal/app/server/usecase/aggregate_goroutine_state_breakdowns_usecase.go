package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// breakdownStepsPerExec は 1 回の Exec で 1 つのインスタンスについて作る分の数
const breakdownStepsPerExec = 30

type AggregateGoroutineStateBreakdownsUsecaseInput struct {
	Now time.Time
}

type AggregateGoroutineStateBreakdownsUsecaseOutput struct {
	Written int
}

type AggregateGoroutineStateBreakdownsUsecase struct {
	inventory   domain.InventoryAccessor
	holdings    domain.HoldingAccessor
	lifecycle   domain.LifecycleAccessor
	checkpoints domain.CheckpointAccessor
	traces      domain.TraceAccessor
	breakdowns  domain.BreakdownAccessor
}

func (us *UseCase) NewAggregateGoroutineStateBreakdownsUsecase() *AggregateGoroutineStateBreakdownsUsecase {
	return &AggregateGoroutineStateBreakdownsUsecase{inventory: us.inventory, holdings: us.holdings, lifecycle: us.lifecycle,
		checkpoints: us.checkpoints, traces: us.traces, breakdowns: us.breakdowns}
}

func (u *AggregateGoroutineStateBreakdownsUsecase) Exec(ctx context.Context, in AggregateGoroutineStateBreakdownsUsecaseInput) (AggregateGoroutineStateBreakdownsUsecaseOutput, error) {
	var out AggregateGoroutineStateBreakdownsUsecaseOutput
	err := eachInstance(ctx, u.inventory, func(s observation.ServiceName, i observation.InstanceName) error {
		n, err := u.aggregate(ctx, s, i)
		out.Written += n
		return err
	})
	if err != nil {
		return AggregateGoroutineStateBreakdownsUsecaseOutput{}, err
	}
	return out, nil
}

func (u *AggregateGoroutineStateBreakdownsUsecase) aggregate(ctx context.Context, s observation.ServiceName, i observation.InstanceName) (int, error) {
	cps, err := u.checkpoints.ReadCheckpoints(ctx, s)
	if err != nil {
		return 0, err
	}
	cp := cps.Get(domain.ProcessAggregateGoroutineStateBreakdowns, string(i))
	h, arrival, err := arrivalOf(ctx, u.holdings, u.lifecycle, s, i)
	if err != nil {
		return 0, err
	}
	steps := domain.CompleteSteps(cp.At, startOf(cp, h, firstLookback), domain.BreakdownStep, arrival, breakdownStepsPerExec)
	if len(steps) == 0 {
		return 0, nil
	}
	written := 0
	for _, minute := range steps {
		end := minute.Add(domain.BreakdownStep)
		chunks, err := u.traces.ListTraceChunks(ctx, s, i, minute, end)
		if err != nil {
			return written, err
		}
		if joined, ok := observation.Join(chunks, minute, end); ok {
			rows := domain.BuildMinuteBreakdowns(joined, minute)
			if len(rows) > 0 {
				if err := u.breakdowns.AppendBreakdowns(ctx, s, i, rows); err != nil {
					return written, err
				}
				written += len(rows)
			}
		}
		if err := u.checkpoints.UpdateCheckpoint(ctx, s, domain.ProcessAggregateGoroutineStateBreakdowns, string(i), domain.Checkpoint{At: minute}); err != nil {
			return written, err
		}
	}
	return written, nil
}
