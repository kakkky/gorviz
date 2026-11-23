package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
)

// ObservationWorker はチャンクを間を空けずに取り続ける。Exec がチャンクの長さだけ待つので、間隔は 0。
// 1 つも取れなかったときだけ、アプリが応じるまで少し待ってから取り直す
type ObservationWorker struct {
	uc    *usecase.CollectObservationsUsecase
	in    usecase.CollectObservationsUsecaseInput
	retry time.Duration
}

func NewObservationWorker(cfg *config.Config, uc *usecase.CollectObservationsUsecase) (*ObservationWorker, error) {
	mode, err := domain.NewMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	return &ObservationWorker{
		uc:    uc,
		in:    usecase.CollectObservationsUsecaseInput{Mode: mode, ChunkLength: cfg.ChunkLength, LeakCheckInterval: cfg.LeakCheckInterval},
		retry: cfg.RetryInterval,
	}, nil
}

func (w *ObservationWorker) Run(ctx context.Context) error {
	for {
		out, err := w.uc.Exec(ctx, w.in)
		if err != nil {
			slog.ErrorContext(ctx, "collect observations", slog.Any("err", err))
		}
		interval := time.Duration(0)
		if out.Collected == 0 {
			interval = w.retry
		}
		if !wait(ctx, interval) {
			return nil
		}
	}
}
