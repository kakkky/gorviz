package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// WriteAppRuntimeTraceEventUsecase は server に言われた出来事 (phase の始まりなど) を execution trace に書く
type WriteAppRuntimeTraceEventUsecase struct {
	trace domain.AppRuntimeTrace
}

type WriteAppRuntimeTraceEventUsecaseInput struct {
	Event string
}

type WriteAppRuntimeTraceEventUsecaseOutput struct{}

func (us *UseCase) NewWriteAppRuntimeTraceEventUsecase() *WriteAppRuntimeTraceEventUsecase {
	return &WriteAppRuntimeTraceEventUsecase{trace: us.trace}
}

func (u *WriteAppRuntimeTraceEventUsecase) Exec(_ context.Context, in WriteAppRuntimeTraceEventUsecaseInput) (WriteAppRuntimeTraceEventUsecaseOutput, error) {
	if in.Event == "" {
		return WriteAppRuntimeTraceEventUsecaseOutput{}, domain.ErrInvalidArgument.With("event is empty")
	}
	u.trace.WriteTraceEvent(in.Event)
	return WriteAppRuntimeTraceEventUsecaseOutput{}, nil
}
