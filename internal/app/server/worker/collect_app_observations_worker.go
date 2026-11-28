package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// CollectAppObservationsWorker は短いほうの間隔 (shutdown の間の間隔) で呼び、今回取るかは usecase に任せる。
// shutdown の間も取るので、ctx の終わりでは抜けず、アプリが終わったら抜ける
type CollectAppObservationsWorker struct {
	collect  *usecase.CollectAppObservationsUsecase
	interval time.Duration
}

func NewCollectAppObservationsWorker(collect *usecase.CollectAppObservationsUsecase) *CollectAppObservationsWorker {
	return &CollectAppObservationsWorker{collect: collect, interval: time.Second}
}

func (w *CollectAppObservationsWorker) Run(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	for wait(ctx, w.interval) {
		out, err := w.collect.Exec(ctx, usecase.CollectAppObservationsUsecaseInput{})
		if err != nil {
			slog.WarnContext(ctx, "collect app observations", slog.Any("err", err))
		}
		if out.AppGone {
			return nil
		}
	}
	return nil
}
