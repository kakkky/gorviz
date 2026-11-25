package worker

import (
	"context"
	"log/slog"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// IndexAppRuntimeTraceWorker は server が起動したときに 1 回だけ IndexAppRuntimeTraceUsecase を呼び、trace が終わるまで待つ。
// server が止まるときも、アプリの shutdown の trace を最後まで読むので、ctx の終わりでは抜けない
type IndexAppRuntimeTraceWorker struct {
	index *usecase.IndexAppRuntimeTraceUsecase
}

func NewIndexAppRuntimeTraceWorker(index *usecase.IndexAppRuntimeTraceUsecase) *IndexAppRuntimeTraceWorker {
	return &IndexAppRuntimeTraceWorker{index: index}
}

func (w *IndexAppRuntimeTraceWorker) Run(ctx context.Context) error {
	out, err := w.index.Exec(ctx, usecase.IndexAppRuntimeTraceUsecaseInput{})
	if err != nil {
		slog.ErrorContext(ctx, "index the execution trace", slog.Any("err", err))
		return nil
	}
	slog.InfoContext(ctx, "execution trace ended", slog.Bool("broken", out.Broken))
	return nil
}
