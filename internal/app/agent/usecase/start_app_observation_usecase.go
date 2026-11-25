package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// StartAppObservationUsecase は server にアプリの素性を伝え、execution trace を取り始める
type StartAppObservationUsecase struct {
	process domain.AppProcess
	trace   domain.AppRuntimeTrace
	sender  domain.ServerSender
}

type StartAppObservationUsecaseInput struct{}

type StartAppObservationUsecaseOutput struct{}

func (us *UseCase) NewStartAppObservationUsecase() *StartAppObservationUsecase {
	return &StartAppObservationUsecase{process: us.process, trace: us.trace, sender: us.sender}
}

func (u *StartAppObservationUsecase) Exec(_ context.Context, _ StartAppObservationUsecaseInput) (StartAppObservationUsecaseOutput, error) {
	if err := u.sender.SendHello(domain.Hello{GoVersion: u.process.GoVersion(), Procs: u.process.Procs(), PID: u.process.PID()}); err != nil {
		return StartAppObservationUsecaseOutput{}, err
	}
	if err := u.trace.StartTrace(); err != nil {
		return StartAppObservationUsecaseOutput{}, err
	}
	return StartAppObservationUsecaseOutput{}, nil
}
