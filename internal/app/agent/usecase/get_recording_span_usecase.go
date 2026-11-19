package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// GetRecordingSpanUsecase は録画が今持てている区間と、その大きさを返す
type GetRecordingSpanUsecase struct {
	traceRecording domain.TraceRecording
}

func (us *UseCase) NewGetRecordingSpanUsecase() *GetRecordingSpanUsecase {
	return &GetRecordingSpanUsecase{traceRecording: us.traceRecording}
}

type GetRecordingSpanUsecaseInput struct{}

type GetRecordingSpanUsecaseOutput struct {
	Span model.RecordingSpan
	OK   bool
}

func (us *GetRecordingSpanUsecase) Exec(_ context.Context, _ GetRecordingSpanUsecaseInput) (GetRecordingSpanUsecaseOutput, error) {
	s, ok := us.traceRecording.Span()
	return GetRecordingSpanUsecaseOutput{Span: s, OK: ok}, nil
}
