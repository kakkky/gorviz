package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

type RecordLogLineUsecase struct {
	logRecording domain.LogRecording
	logParser    domain.LogParser
}

func (us *UseCase) NewRecordLogLineUsecase() *RecordLogLineUsecase {
	return &RecordLogLineUsecase{logRecording: us.logRecording, logParser: us.logParser}
}

type RecordLogLineUsecaseInput struct {
	Raw        string
	Source     string
	ReceivedAt time.Time
}

type RecordLogLineUsecaseOutput struct {
	Line model.LogLine
}

func (us *RecordLogLineUsecase) Exec(_ context.Context, in RecordLogLineUsecaseInput) (RecordLogLineUsecaseOutput, error) {
	line := us.logRecording.Append(us.logParser.Parse(in.Raw, in.Source, in.ReceivedAt))
	return RecordLogLineUsecaseOutput{Line: line}, nil
}
