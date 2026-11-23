package worker

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type AggregateMetricsWorker struct {
	uc       *usecase.AggregateMetricsUsecase
	interval time.Duration
}

func NewAggregateMetricsWorker(cfg *config.Config, uc *usecase.AggregateMetricsUsecase) *AggregateMetricsWorker {
	return &AggregateMetricsWorker{uc: uc, interval: cfg.AggregateMetricsInterval}
}

func (w *AggregateMetricsWorker) Run(ctx context.Context) error {
	return loop(ctx, "aggregate_metrics", w.interval, func(ctx context.Context, now time.Time) error {
		_, err := w.uc.Exec(ctx, usecase.AggregateMetricsUsecaseInput{Now: now})
		return err
	})
}
