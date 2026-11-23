package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// metricStepsPerExec は 1 回の Exec で 1 つのインスタンスについて作る 10 秒の数 (1 時間分)
const metricStepsPerExec = 360

type AggregateMetricsUsecaseInput struct {
	Now time.Time
}

type AggregateMetricsUsecaseOutput struct {
	Written int
}

type AggregateMetricsUsecase struct {
	inventory   domain.InventoryAccessor
	holdings    domain.HoldingAccessor
	lifecycle   domain.LifecycleAccessor
	checkpoints domain.CheckpointAccessor
	traces      domain.TraceAccessor
	logs        domain.LogAccessor
	snapshots   domain.SnapshotAccessor
	metrics     domain.MetricAccessor
}

func (us *UseCase) NewAggregateMetricsUsecase() *AggregateMetricsUsecase {
	return &AggregateMetricsUsecase{inventory: us.inventory, holdings: us.holdings, lifecycle: us.lifecycle, checkpoints: us.checkpoints,
		traces: us.traces, logs: us.logs, snapshots: us.snapshots, metrics: us.metrics}
}

func (u *AggregateMetricsUsecase) Exec(ctx context.Context, in AggregateMetricsUsecaseInput) (AggregateMetricsUsecaseOutput, error) {
	var out AggregateMetricsUsecaseOutput
	err := eachInstance(ctx, u.inventory, func(s observation.ServiceName, i observation.InstanceName) error {
		n, err := u.aggregate(ctx, s, i)
		out.Written += n
		return err
	})
	if err != nil {
		return AggregateMetricsUsecaseOutput{}, err
	}
	return out, nil
}

func (u *AggregateMetricsUsecase) aggregate(ctx context.Context, s observation.ServiceName, i observation.InstanceName) (int, error) {
	cps, err := u.checkpoints.ReadCheckpoints(ctx, s)
	if err != nil {
		return 0, err
	}
	cp := cps.Get(domain.ProcessAggregateMetrics, string(i))
	h, arrival, err := arrivalOf(ctx, u.holdings, u.lifecycle, s, i)
	if err != nil {
		return 0, err
	}
	steps := domain.CompleteSteps(cp.At, startOf(cp, h, firstLookback), domain.MetricStep, arrival, metricStepsPerExec)
	if len(steps) == 0 {
		return 0, nil
	}
	from, to := steps[0], steps[len(steps)-1].Add(domain.MetricStep)
	chunks, err := u.traces.ListTraceChunks(ctx, s, i, from, to)
	if err != nil {
		return 0, err
	}
	logs, err := u.logs.ListLogs(ctx, s, from, to)
	if err != nil {
		return 0, err
	}
	snapshots, err := u.snapshots.ListSnapshots(ctx, s, i, from, to)
	if err != nil {
		return 0, err
	}
	var samples []domain.MetricSample
	for _, at := range steps {
		end := at.Add(domain.MetricStep)
		var (
			cs []observation.TraceChunk
			ls []observation.LogLine
			ss []observation.GoroutineSnapshot
		)
		for _, c := range chunks {
			if inStep(c.StartedAt, at, end) {
				cs = append(cs, c)
			}
		}
		for _, l := range logs {
			if l.Instance == i && inStep(l.At, at, end) {
				ls = append(ls, l.LogLine)
			}
		}
		for _, sn := range snapshots {
			if inStep(sn.At, at, end) {
				ss = append(ss, sn)
			}
		}
		if len(cs) == 0 && len(ls) == 0 && len(ss) == 0 {
			continue
		}
		samples = append(samples, domain.AggregateMetric(i, at, cs, ls, ss))
	}
	if len(samples) > 0 {
		if err := u.metrics.AppendMetrics(ctx, s, samples); err != nil {
			return 0, err
		}
	}
	return len(samples), u.checkpoints.UpdateCheckpoint(ctx, s, domain.ProcessAggregateMetrics, string(i), domain.Checkpoint{At: steps[len(steps)-1]})
}

func inStep(t, from, to time.Time) bool {
	return !t.Before(from) && t.Before(to)
}
