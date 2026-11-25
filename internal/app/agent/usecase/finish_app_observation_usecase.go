package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// FinishAppObservationUsecase は最後の goroutineleak プロファイルを送り、trace を止めて書き切り、coverage と終わりの合図を送って接続を閉じる。
// アプリの main の終わりと server の頼みのどちらからも呼ばれるので、2 度目以降は 1 度目が終わるのを待つだけにする
type FinishAppObservationUsecase struct {
	trace      domain.AppRuntimeTrace
	coverage   domain.AppCoverage
	sender     domain.ServerSender
	connection domain.ServerConnection
	leaks      *SendAppGoroutineLeaksUsecase
}

type FinishAppObservationUsecaseInput struct{}

type FinishAppObservationUsecaseOutput struct{}

func (us *UseCase) NewFinishAppObservationUsecase() *FinishAppObservationUsecase {
	return &FinishAppObservationUsecase{trace: us.trace, coverage: us.coverage, sender: us.sender, connection: us.connection, leaks: us.NewSendAppGoroutineLeaksUsecase()}
}

func (u *FinishAppObservationUsecase) Exec(ctx context.Context, _ FinishAppObservationUsecaseInput) (FinishAppObservationUsecaseOutput, error) {
	if !u.connection.BeginFinish() {
		u.connection.WaitFinished()
		return FinishAppObservationUsecaseOutput{}, nil
	}
	defer u.connection.Close()
	u.trace.WriteTraceEvent(domain.AppExitEvent)
	_, leakErr := u.leaks.Exec(ctx, SendAppGoroutineLeaksUsecaseInput{})
	u.trace.StopTrace()
	u.trace.WaitTraceDrained()
	if files, err := u.coverage.ReadCoverage(); err == nil {
		if err := u.sender.SendCoverage(files); err != nil {
			return FinishAppObservationUsecaseOutput{}, err
		}
	}
	if err := u.sender.SendBye(); err != nil {
		return FinishAppObservationUsecaseOutput{}, err
	}
	return FinishAppObservationUsecaseOutput{}, leakErr
}
