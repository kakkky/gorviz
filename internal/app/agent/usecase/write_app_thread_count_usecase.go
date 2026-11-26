package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// WriteAppThreadCountUsecase は OS のスレッドの数を execution trace に書く。trace に書けば、数えた時刻ごと server へ流れる
type WriteAppThreadCountUsecase struct {
	trace      domain.AppRuntimeTrace
	goroutines domain.AppRuntimeGoroutines
}

type WriteAppThreadCountUsecaseInput struct{}

type WriteAppThreadCountUsecaseOutput struct{}

func (us *UseCase) NewWriteAppThreadCountUsecase() *WriteAppThreadCountUsecase {
	return &WriteAppThreadCountUsecase{trace: us.trace, goroutines: us.goroutines}
}

func (u *WriteAppThreadCountUsecase) Exec(_ context.Context, _ WriteAppThreadCountUsecaseInput) (WriteAppThreadCountUsecaseOutput, error) {
	u.trace.WriteTraceEvent(domain.ThreadCountEvent(u.goroutines.ThreadCount()))
	return WriteAppThreadCountUsecaseOutput{}, nil
}
