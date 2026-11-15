package recorder

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/usecase"
)

// 対象に繋がらないときに、取り直しまで待つ時間
const retryInterval = time.Second

// Recorder は対象の観測を裏で回し続ける。画面 (Live / Explore / CLI) は自分では観測せず、ここが貯めたものを読む:
//   - trace の区切りを間を空けずに取り続ける。trace は対象のプロセスで同時に 1 本しか取れないので、gorviz の中でここだけが取る
//   - refresh ごとに画面用のスナップショットを作る。goroutine dump はそのうち dump-interval ごとにだけ取る
type Recorder struct {
	recordTraceChunkUsecase      *usecase.RecordTraceChunkUsecase
	recordRuntimeSnapshotUsecase *usecase.RecordRuntimeSnapshotUsecase
	refresh                      time.Duration
}

func NewRecorder(cfg *config.Config, uc *usecase.UseCase) *Recorder {
	return &Recorder{
		recordTraceChunkUsecase:      uc.NewRecordTraceChunkUsecase(),
		recordRuntimeSnapshotUsecase: uc.NewRecordRuntimeSnapshotUsecase(),
		refresh:                      cfg.Refresh,
	}
}

func (r *Recorder) Run(ctx context.Context) error {
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(r.recordTraces)
		sc.Go(r.recordSnapshots)
		return nil
	})
}

func (r *Recorder) recordTraces(ctx context.Context) error {
	for ctx.Err() == nil {
		if _, err := r.recordTraceChunkUsecase.Exec(ctx, usecase.RecordTraceChunkUsecaseInput{}); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.WarnContext(ctx, "record trace chunk", slog.Any("err", err))
			sleep(ctx, retryInterval)
		}
	}
	return nil
}

func (r *Recorder) recordSnapshots(ctx context.Context) error {
	ticker := time.NewTicker(r.refresh)
	defer ticker.Stop()
	for {
		if _, err := r.recordRuntimeSnapshotUsecase.Exec(ctx, usecase.RecordRuntimeSnapshotUsecaseInput{Now: time.Now()}); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "record runtime snapshot", slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
