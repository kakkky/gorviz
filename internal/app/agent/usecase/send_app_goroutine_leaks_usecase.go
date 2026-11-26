package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// SendAppGoroutineLeaksUsecase は goroutineleak プロファイルを書き出して server へ送る。
// 書き出す直前に trace へ印を書き、プロファイルを取った時刻を server が trace の上で知れるようにする
type SendAppGoroutineLeaksUsecase struct {
	trace      domain.AppRuntimeTrace
	goroutines domain.AppRuntimeGoroutines
	sender     domain.ServerSender
}

type SendAppGoroutineLeaksUsecaseInput struct{}

type SendAppGoroutineLeaksUsecaseOutput struct{}

func (us *UseCase) NewSendAppGoroutineLeaksUsecase() *SendAppGoroutineLeaksUsecase {
	return &SendAppGoroutineLeaksUsecase{trace: us.trace, goroutines: us.goroutines, sender: us.sender}
}

func (u *SendAppGoroutineLeaksUsecase) Exec(_ context.Context, _ SendAppGoroutineLeaksUsecaseInput) (SendAppGoroutineLeaksUsecaseOutput, error) {
	seq := u.goroutines.NextLeakCheckSeq()
	u.trace.WriteTraceEvent(domain.LeakCheckEvent(seq))
	text, err := u.goroutines.ReadGoroutineLeakProfile()
	if err != nil {
		return SendAppGoroutineLeaksUsecaseOutput{}, err
	}
	if err := u.sender.SendGoroutineLeakProfile(seq, text); err != nil {
		return SendAppGoroutineLeaksUsecaseOutput{}, err
	}
	return SendAppGoroutineLeaksUsecaseOutput{}, nil
}
