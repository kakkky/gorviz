package usecase

import (
	"context"

	"github.com/kakkky/gorviz/domain"
)

type GetSchedTraceUsecase struct {
	schedTrace domain.SchedTraceSource
}

func (us *UseCase) NewGetSchedTraceUsecase() *GetSchedTraceUsecase {
	return &GetSchedTraceUsecase{schedTrace: us.schedTrace}
}

type GetSchedTraceUsecaseInput struct{}

type GetSchedTraceUsecaseOutput struct {
	Report    domain.SchedTraceReport
	Available bool
}

func (us *GetSchedTraceUsecase) Exec(_ context.Context, _ GetSchedTraceUsecaseInput) (GetSchedTraceUsecaseOutput, error) {
	report, ok := us.schedTrace.Latest()
	return GetSchedTraceUsecaseOutput{Report: report, Available: ok}, nil
}
