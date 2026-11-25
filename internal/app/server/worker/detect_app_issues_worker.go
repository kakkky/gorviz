package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// DetectAppIssuesWorker は index に届いた分を 1 秒ごとに判定させる
type DetectAppIssuesWorker struct {
	detect   *usecase.DetectAppIssuesUsecase
	interval time.Duration
}

func NewDetectAppIssuesWorker(detect *usecase.DetectAppIssuesUsecase) *DetectAppIssuesWorker {
	return &DetectAppIssuesWorker{detect: detect, interval: time.Second}
}

func (w *DetectAppIssuesWorker) Run(ctx context.Context) error {
	for wait(ctx, w.interval) {
		if _, err := w.detect.Exec(ctx, usecase.DetectAppIssuesUsecaseInput{}); err != nil {
			slog.WarnContext(ctx, "detect app issues", slog.Any("err", err))
		}
	}
	return nil
}
