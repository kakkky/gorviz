package worker

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type AggregateGoroutineStateBreakdownsWorker struct {
	uc       *usecase.AggregateGoroutineStateBreakdownsUsecase
	interval time.Duration
}

func NewAggregateGoroutineStateBreakdownsWorker(cfg *config.Config, uc *usecase.AggregateGoroutineStateBreakdownsUsecase) *AggregateGoroutineStateBreakdownsWorker {
	return &AggregateGoroutineStateBreakdownsWorker{uc: uc, interval: cfg.AggregateGoroutineStateBreakdownsInterval}
}

func (w *AggregateGoroutineStateBreakdownsWorker) Run(ctx context.Context) error {
	return loop(ctx, "aggregate_goroutine_state_breakdowns", w.interval, func(ctx context.Context, now time.Time) error {
		_, err := w.uc.Exec(ctx, usecase.AggregateGoroutineStateBreakdownsUsecaseInput{Now: now})
		return err
	})
}
