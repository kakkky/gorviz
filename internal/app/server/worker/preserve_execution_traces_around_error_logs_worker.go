package worker

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type PreserveExecutionTracesAroundErrorLogsWorker struct {
	uc       *usecase.PreserveExecutionTracesAroundErrorLogsUsecase
	interval time.Duration
}

func NewPreserveExecutionTracesAroundErrorLogsWorker(cfg *config.Config, uc *usecase.PreserveExecutionTracesAroundErrorLogsUsecase) *PreserveExecutionTracesAroundErrorLogsWorker {
	return &PreserveExecutionTracesAroundErrorLogsWorker{uc: uc, interval: cfg.PreserveExecutionTracesAroundErrorLogsInterval}
}

func (w *PreserveExecutionTracesAroundErrorLogsWorker) Run(ctx context.Context) error {
	return loop(ctx, "preserve_execution_traces_around_error_logs", w.interval, func(ctx context.Context, now time.Time) error {
		_, err := w.uc.Exec(ctx, usecase.PreserveExecutionTracesAroundErrorLogsUsecaseInput{Now: now})
		return err
	})
}
