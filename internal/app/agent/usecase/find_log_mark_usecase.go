package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

// FindLogMarkUsecase は gorviz のロガーの差し込み口が Seq を書いた区切りと、その記録を返す
type FindLogMarkUsecase struct {
	traceRecording domain.TraceRecording
}

func (us *UseCase) NewFindLogMarkUsecase() *FindLogMarkUsecase {
	return &FindLogMarkUsecase{traceRecording: us.traceRecording}
}

type FindLogMarkUsecaseInput struct {
	Seq uint64
}

type FindLogMarkUsecaseOutput struct {
	Chunk model.TraceChunk
	Mark  model.TraceLogMark
	OK    bool
}

func (us *FindLogMarkUsecase) Exec(_ context.Context, in FindLogMarkUsecaseInput) (FindLogMarkUsecaseOutput, error) {
	c, m, ok := us.traceRecording.FindLogMark(in.Seq)
	return FindLogMarkUsecaseOutput{Chunk: c, Mark: m, OK: ok}, nil
}
