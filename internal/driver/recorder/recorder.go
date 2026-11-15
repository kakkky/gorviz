package recorder

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/config"
	"github.com/kakkky/gorviz/internal/usecase"
)

// 対象に繋がらないときに、取り直しまで待つ時間
const retryInterval = time.Second

// Recorder は agent の側で対象の観測を裏で回し続ける。server は自分では観測せず、ここが貯めたものを読む:
//   - trace の区切りを取り続ける。trace は対象のプロセスで同時に 1 本しか取れないので、gorviz の中でここだけが取る
//   - dump-interval ごとに goroutine dump を取る
type Recorder struct {
	recordTraceChunkUsecase *usecase.RecordTraceChunkUsecase
	recordDumpUsecase       *usecase.RecordDumpUsecase
	dumpInterval            time.Duration
	intermittent            bool
	recordEvery             time.Duration
	recordFor               time.Duration
}

func NewRecorder(cfg *config.Config, uc *usecase.UseCase) *Recorder {
	return &Recorder{
		recordTraceChunkUsecase: uc.NewRecordTraceChunkUsecase(),
		recordDumpUsecase:       uc.NewRecordDumpUsecase(),
		dumpInterval:            cfg.DumpInterval,
		intermittent:            cfg.RecordMode == "intermittent",
		recordEvery:             cfg.RecordEvery,
		recordFor:               cfg.RecordFor,
	}
}

func (r *Recorder) Run(ctx context.Context) error {
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(r.recordTraces)
		sc.Go(r.recordDumps)
		return nil
	})
}

// recordTraces は常時なら区切りを間を空けずに取り続ける。間欠なら recordEvery ごとに recordFor の間だけ取る
func (r *Recorder) recordTraces(ctx context.Context) error {
	for ctx.Err() == nil {
		started := time.Now()
		for ctx.Err() == nil && (!r.intermittent || time.Since(started) < r.recordFor) {
			if _, err := r.recordTraceChunkUsecase.Exec(ctx, usecase.RecordTraceChunkUsecaseInput{}); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				slog.WarnContext(ctx, "record trace chunk", slog.Any("err", err))
				sleep(ctx, retryInterval)
			}
		}
		if r.intermittent {
			sleep(ctx, r.recordEvery-time.Since(started))
		}
	}
	return nil
}

func (r *Recorder) recordDumps(ctx context.Context) error {
	ticker := time.NewTicker(r.dumpInterval)
	defer ticker.Stop()
	for {
		if _, err := r.recordDumpUsecase.Exec(ctx, usecase.RecordDumpUsecaseInput{Now: time.Now()}); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "record goroutine dump", slog.Any("err", err))
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
