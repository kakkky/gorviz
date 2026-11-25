package worker

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/usecase"
)

// StreamAppRuntimeTraceWorker は trace が流れ切るまで、間を空けずに読んで送る
type StreamAppRuntimeTraceWorker struct {
	stream *usecase.StreamAppRuntimeTraceUsecase
}

func NewStreamAppRuntimeTraceWorker(stream *usecase.StreamAppRuntimeTraceUsecase) *StreamAppRuntimeTraceWorker {
	return &StreamAppRuntimeTraceWorker{stream: stream}
}

func (w *StreamAppRuntimeTraceWorker) Run(ctx context.Context) error {
	for {
		out, err := w.stream.Exec(ctx, usecase.StreamAppRuntimeTraceUsecaseInput{})
		if err == nil && out.Ended {
			return nil
		}
	}
}
