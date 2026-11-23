package worker

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type PruneDataWorker struct {
	uc       *usecase.PruneDataUsecase
	interval time.Duration
}

func NewPruneDataWorker(cfg *config.Config, uc *usecase.PruneDataUsecase) *PruneDataWorker {
	return &PruneDataWorker{uc: uc, interval: cfg.PruneDataInterval}
}

func (w *PruneDataWorker) Run(ctx context.Context) error {
	return loop(ctx, "prune_data", w.interval, func(ctx context.Context, now time.Time) error {
		_, err := w.uc.Exec(ctx, usecase.PruneDataUsecaseInput{Now: now})
		return err
	})
}
