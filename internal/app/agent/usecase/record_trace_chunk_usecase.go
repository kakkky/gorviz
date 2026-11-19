package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

type RecordTraceChunkUsecase struct {
	runtimeClient  domain.RuntimeClient
	traceRecording domain.TraceRecording
	chunkDuration  time.Duration
}

func (us *UseCase) NewRecordTraceChunkUsecase() *RecordTraceChunkUsecase {
	return &RecordTraceChunkUsecase{
		runtimeClient:  us.runtimeClient,
		traceRecording: us.traceRecording,
		chunkDuration:  us.settings.ChunkDuration,
	}
}

type RecordTraceChunkUsecaseInput struct{}

type RecordTraceChunkUsecaseOutput struct {
	Chunk model.TraceChunk
}

// Exec は区切り 1 つ分の trace を取って録画に足す。録画ループから間を空けずに繰り返し呼ばれる
func (us *RecordTraceChunkUsecase) Exec(ctx context.Context, _ RecordTraceChunkUsecaseInput) (RecordTraceChunkUsecaseOutput, error) {
	chunk, err := us.runtimeClient.FetchTraceChunk(ctx, us.chunkDuration)
	if err != nil {
		return RecordTraceChunkUsecaseOutput{}, err
	}
	us.traceRecording.Append(chunk)
	return RecordTraceChunkUsecaseOutput{Chunk: chunk}, nil
}
