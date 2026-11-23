package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const (
	logsPerAttach = 5000
	// logMarkSlack はログに書かれた時刻と、trace のログの印の時刻のずれの見込み
	logMarkSlack = 2 * time.Second
)

type AttachLogWriterGoroutinesUsecaseInput struct {
	Now time.Time
}

type AttachLogWriterGoroutinesUsecaseOutput struct {
	Attached int
}

type AttachLogWriterGoroutinesUsecase struct {
	inventory   domain.InventoryAccessor
	holdings    domain.HoldingAccessor
	lifecycle   domain.LifecycleAccessor
	checkpoints domain.CheckpointAccessor
	logs        domain.LogAccessor
	traces      domain.TraceAccessor
	logWriters  domain.LogWriterAccessor
}

func (us *UseCase) NewAttachLogWriterGoroutinesUsecase() *AttachLogWriterGoroutinesUsecase {
	return &AttachLogWriterGoroutinesUsecase{inventory: us.inventory, holdings: us.holdings, lifecycle: us.lifecycle,
		checkpoints: us.checkpoints, logs: us.logs, traces: us.traces, logWriters: us.logWriters}
}

func (u *AttachLogWriterGoroutinesUsecase) Exec(ctx context.Context, in AttachLogWriterGoroutinesUsecaseInput) (AttachLogWriterGoroutinesUsecaseOutput, error) {
	var out AttachLogWriterGoroutinesUsecaseOutput
	err := eachInstance(ctx, u.inventory, func(s observation.ServiceName, i observation.InstanceName) error {
		n, err := u.attach(ctx, s, i)
		out.Attached += n
		return err
	})
	if err != nil {
		return AttachLogWriterGoroutinesUsecaseOutput{}, err
	}
	return out, nil
}

func (u *AttachLogWriterGoroutinesUsecase) attach(ctx context.Context, s observation.ServiceName, i observation.InstanceName) (int, error) {
	cps, err := u.checkpoints.ReadCheckpoints(ctx, s)
	if err != nil {
		return 0, err
	}
	cp := cps.Get(domain.ProcessAttachLogWriterGoroutines, string(i))
	h, arrival, err := arrivalOf(ctx, u.holdings, u.lifecycle, s, i)
	if err != nil || h.Latest.IsZero() {
		return 0, err
	}
	logs, err := instanceLogs(ctx, u.logs, s, i, cp, h)
	if err != nil {
		return 0, err
	}
	var ready []domain.StoredLog
	for _, l := range logs {
		if len(ready) >= logsPerAttach || !arrival.MaterialReady(l.At.Add(logMarkSlack)) {
			break
		}
		ready = append(ready, l)
	}
	if len(ready) == 0 {
		return 0, nil
	}
	chunks, err := u.traces.ListTraceChunks(ctx, s, i, ready[0].At.Add(-logMarkSlack), ready[len(ready)-1].At.Add(logMarkSlack))
	if err != nil {
		return 0, err
	}
	writers := make([]domain.LogWriter, 0, len(ready))
	for _, l := range ready {
		var near []observation.TraceChunk
		for _, c := range chunks {
			if c.Overlaps(l.At.Add(-logMarkSlack), l.At.Add(logMarkSlack)) {
				near = append(near, c)
			}
		}
		writers = append(writers, domain.MatchLogWriter(i, l.LogLine, near))
	}
	if err := u.logWriters.AppendLogWriters(ctx, s, i, writers); err != nil {
		return 0, err
	}
	return len(writers), u.checkpoints.UpdateCheckpoint(ctx, s, domain.ProcessAttachLogWriterGoroutines, string(i), logCheckpoint(ready[len(ready)-1]))
}
