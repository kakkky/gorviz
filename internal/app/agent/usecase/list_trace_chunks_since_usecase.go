package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// ListTraceChunksSinceUsecase は After より後に始まった区切りを、古い順に Limit 個まで返す。server へ送るのに使う
type ListTraceChunksSinceUsecase struct {
	traceRecording domain.TraceRecording
}

func (us *UseCase) NewListTraceChunksSinceUsecase() *ListTraceChunksSinceUsecase {
	return &ListTraceChunksSinceUsecase{traceRecording: us.traceRecording}
}

type ListTraceChunksSinceUsecaseInput struct {
	After time.Time
	Limit int
}

type ListTraceChunksSinceUsecaseOutput struct {
	Chunks []model.TraceChunk
}

func (us *ListTraceChunksSinceUsecase) Exec(_ context.Context, in ListTraceChunksSinceUsecaseInput) (ListTraceChunksSinceUsecaseOutput, error) {
	return ListTraceChunksSinceUsecaseOutput{Chunks: us.traceRecording.Since(in.After, in.Limit)}, nil
}
