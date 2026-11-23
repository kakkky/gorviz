package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const errorLogsPerExec = 200

type PreserveExecutionTracesAroundErrorLogsUsecaseInput struct {
	Now time.Time
}

type PreserveExecutionTracesAroundErrorLogsUsecaseOutput struct {
	Preserved int
}

type PreserveExecutionTracesAroundErrorLogsUsecase struct {
	inventory   domain.InventoryAccessor
	holdings    domain.HoldingAccessor
	lifecycle   domain.LifecycleAccessor
	checkpoints domain.CheckpointAccessor
	logs        domain.LogAccessor
	traces      domain.TraceAccessor
	preserved   domain.PreservedTraceAccessor
}

func (us *UseCase) NewPreserveExecutionTracesAroundErrorLogsUsecase() *PreserveExecutionTracesAroundErrorLogsUsecase {
	return &PreserveExecutionTracesAroundErrorLogsUsecase{inventory: us.inventory, holdings: us.holdings, lifecycle: us.lifecycle,
		checkpoints: us.checkpoints, logs: us.logs, traces: us.traces, preserved: us.preserved}
}

func (u *PreserveExecutionTracesAroundErrorLogsUsecase) Exec(ctx context.Context, in PreserveExecutionTracesAroundErrorLogsUsecaseInput) (PreserveExecutionTracesAroundErrorLogsUsecaseOutput, error) {
	var out PreserveExecutionTracesAroundErrorLogsUsecaseOutput
	err := eachInstance(ctx, u.inventory, func(s observation.ServiceName, i observation.InstanceName) error {
		n, err := u.preserve(ctx, s, i)
		out.Preserved += n
		return err
	})
	if err != nil {
		return PreserveExecutionTracesAroundErrorLogsUsecaseOutput{}, err
	}
	return out, nil
}

// instanceLogs はチェックポイントより後の、インスタンスのログ
func instanceLogs(ctx context.Context, logs domain.LogAccessor, s observation.ServiceName, i observation.InstanceName, cp domain.Checkpoint, h domain.Holding) ([]domain.StoredLog, error) {
	from := startOf(cp, h, firstLookback)
	all, err := logs.ListLogs(ctx, s, from, h.Latest.Add(time.Nanosecond))
	if err != nil {
		return nil, err
	}
	var out []domain.StoredLog
	for _, l := range all {
		if l.Instance == i && cp.AfterLog(l.LogLine) {
			out = append(out, l)
		}
	}
	return out, nil
}

func logCheckpoint(l domain.StoredLog) domain.Checkpoint {
	return domain.Checkpoint{At: l.At, LogAgent: l.Agent, LogSeq: l.Seq}
}

func (u *PreserveExecutionTracesAroundErrorLogsUsecase) preserve(ctx context.Context, s observation.ServiceName, i observation.InstanceName) (int, error) {
	cps, err := u.checkpoints.ReadCheckpoints(ctx, s)
	if err != nil {
		return 0, err
	}
	cp := cps.Get(domain.ProcessPreserveExecutionTracesAroundError, string(i))
	h, arrival, err := arrivalOf(ctx, u.holdings, u.lifecycle, s, i)
	if err != nil || h.Latest.IsZero() {
		return 0, err
	}
	logs, err := instanceLogs(ctx, u.logs, s, i, cp, h)
	if err != nil {
		return 0, err
	}
	next, preserved, errorLogs := cp, 0, 0
	for _, l := range logs {
		if !l.Level.AtLeast(observation.LogLevelError) {
			next = logCheckpoint(l)
			continue
		}
		from, to := domain.AroundErrorLog(l.LogLine)
		if !arrival.MaterialReady(to) || errorLogs >= errorLogsPerExec {
			break
		}
		errorLogs++
		dir := domain.FileName(domain.MessageShape(l.Message))
		existing, err := u.preserved.ListPreservedTraces(ctx, s, i, domain.PreservedAroundErrorLog, domain.DataExecutionTracesAroundErrorLogs.PeriodStart(l.At), dir)
		if err != nil {
			return preserved, err
		}
		if f, t, ok := domain.Uncovered(from, to, existing); ok {
			chunks, err := u.traces.ListTraceChunks(ctx, s, i, f, t)
			if err != nil {
				return preserved, err
			}
			if joined, ok := observation.Join(chunks, f, t); ok {
				p := domain.PreservedTrace{Kind: domain.PreservedAroundErrorLog, Dir: dir, At: l.At, From: joined.StartedAt, To: joined.EndedAt()}
				if err := u.preserved.SavePreservedTrace(ctx, s, i, p, joined); err != nil {
					return preserved, err
				}
				preserved++
			}
		}
		next = logCheckpoint(l)
	}
	if next.At.Equal(cp.At) && next.LogSeq == cp.LogSeq && next.LogAgent == cp.LogAgent {
		return preserved, nil
	}
	return preserved, u.checkpoints.UpdateCheckpoint(ctx, s, domain.ProcessPreserveExecutionTracesAroundError, string(i), next)
}
