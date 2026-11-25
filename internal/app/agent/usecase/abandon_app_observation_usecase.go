package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// AbandonAppObservationUsecase は server との接続が切れたとき、trace を止めてそれきりにする。送り直しはしない
type AbandonAppObservationUsecase struct {
	trace domain.AppRuntimeTrace
}

type AbandonAppObservationUsecaseInput struct{}

type AbandonAppObservationUsecaseOutput struct{}

func (us *UseCase) NewAbandonAppObservationUsecase() *AbandonAppObservationUsecase {
	return &AbandonAppObservationUsecase{trace: us.trace}
}

func (u *AbandonAppObservationUsecase) Exec(_ context.Context, _ AbandonAppObservationUsecaseInput) (AbandonAppObservationUsecaseOutput, error) {
	u.trace.StopTrace()
	return AbandonAppObservationUsecaseOutput{}, nil
}
