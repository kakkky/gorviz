package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// CollectAppObservationsWorker は短いほうの間隔 (スレッドの数) で呼び、今回なにを書き出させるかは usecase に任せる
type CollectAppObservationsWorker struct {
	collect  *usecase.CollectAppObservationsUsecase
	interval time.Duration
}

func NewCollectAppObservationsWorker(collect *usecase.CollectAppObservationsUsecase) *CollectAppObservationsWorker {
	return &CollectAppObservationsWorker{collect: collect, interval: time.Second}
}

func (w *CollectAppObservationsWorker) Run(ctx context.Context) error {
	for wait(ctx, w.interval) {
		if _, err := w.collect.Exec(ctx, usecase.CollectAppObservationsUsecaseInput{}); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "collect app observations", slog.Any("err", err))
		}
	}
	return nil
}
