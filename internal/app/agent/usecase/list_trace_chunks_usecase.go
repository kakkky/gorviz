package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

// ListTraceChunksUsecase は [From, To] に重なる区切りを古い順に返す
type ListTraceChunksUsecase struct {
	traceRecording domain.TraceRecording
}

func (us *UseCase) NewListTraceChunksUsecase() *ListTraceChunksUsecase {
	return &ListTraceChunksUsecase{traceRecording: us.traceRecording}
}

type ListTraceChunksUsecaseInput struct {
	From time.Time
	To   time.Time
}

type ListTraceChunksUsecaseOutput struct {
	Chunks []model.TraceChunk
}

func (us *ListTraceChunksUsecase) Exec(_ context.Context, in ListTraceChunksUsecaseInput) (ListTraceChunksUsecaseOutput, error) {
	return ListTraceChunksUsecaseOutput{Chunks: us.traceRecording.Between(in.From, in.To)}, nil
}
