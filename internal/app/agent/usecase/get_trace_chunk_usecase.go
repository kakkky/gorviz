package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

// GetTraceChunkUsecase は録画から区切りを 1 つ返す。At が空なら最新の区切り
type GetTraceChunkUsecase struct {
	traceRecording domain.TraceRecording
}

func (us *UseCase) NewGetTraceChunkUsecase() *GetTraceChunkUsecase {
	return &GetTraceChunkUsecase{traceRecording: us.traceRecording}
}

type GetTraceChunkUsecaseInput struct {
	At time.Time
}

type GetTraceChunkUsecaseOutput struct {
	Chunk model.TraceChunk
	OK    bool
}

func (us *GetTraceChunkUsecase) Exec(_ context.Context, in GetTraceChunkUsecaseInput) (GetTraceChunkUsecaseOutput, error) {
	if in.At.IsZero() {
		c, ok := us.traceRecording.Latest()
		return GetTraceChunkUsecaseOutput{Chunk: c, OK: ok}, nil
	}
	c, ok := us.traceRecording.At(in.At)
	return GetTraceChunkUsecaseOutput{Chunk: c, OK: ok}, nil
}
