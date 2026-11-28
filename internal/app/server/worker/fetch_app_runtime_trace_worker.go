package worker

import (
	"context"
	"log/slog"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// FetchAppRuntimeTraceWorker は server が起動したときに 1 回だけ FetchAppRuntimeTraceUsecase を呼び、アプリが終わるまで待つ。
// server が止まるときも、アプリの shutdown の trace を取り続けるので、ctx の終わりでは抜けない
type FetchAppRuntimeTraceWorker struct {
	fetch *usecase.FetchAppRuntimeTraceUsecase
}

func NewFetchAppRuntimeTraceWorker(fetch *usecase.FetchAppRuntimeTraceUsecase) *FetchAppRuntimeTraceWorker {
	return &FetchAppRuntimeTraceWorker{fetch: fetch}
}

func (w *FetchAppRuntimeTraceWorker) Run(ctx context.Context) error {
	out, err := w.fetch.Exec(context.WithoutCancel(ctx), usecase.FetchAppRuntimeTraceUsecaseInput{})
	if err != nil {
		slog.ErrorContext(ctx, "fetch the execution trace", slog.Any("err", err))
		return nil
	}
	slog.InfoContext(ctx, "the app stopped serving the execution trace", slog.Int("chunks", out.Chunks))
	return nil
}
