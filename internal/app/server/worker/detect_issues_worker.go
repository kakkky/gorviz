package worker

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type DetectIssuesWorker struct {
	uc       *usecase.DetectIssuesUsecase
	interval time.Duration
}

func NewDetectIssuesWorker(cfg *config.Config, uc *usecase.DetectIssuesUsecase) *DetectIssuesWorker {
	return &DetectIssuesWorker{uc: uc, interval: cfg.DetectIssuesInterval}
}

func (w *DetectIssuesWorker) Run(ctx context.Context) error {
	return loop(ctx, "detect_issues", w.interval, func(ctx context.Context, now time.Time) error {
		_, err := w.uc.Exec(ctx, usecase.DetectIssuesUsecaseInput{Now: now})
		return err
	})
}
