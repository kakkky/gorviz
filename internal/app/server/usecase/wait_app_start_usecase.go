package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const startWait = 10 * time.Minute

// WaitAppStartUsecase はアプリがビルドされ、trace が取れ始めるまで待つ。ビルドや起動に失敗したら、そこで返す
type WaitAppStartUsecase struct {
	sessions domain.SessionStore
	clock    domain.Clock
}

type WaitAppStartUsecaseInput struct{}

type WaitAppStartUsecaseOutput struct {
	App     domain.AppRun
	Started bool
}

func (us *UseCase) NewWaitAppStartUsecase() *WaitAppStartUsecase {
	return &WaitAppStartUsecase{sessions: us.sessions, clock: us.clock}
}

func (u *WaitAppStartUsecase) Exec(ctx context.Context, _ WaitAppStartUsecaseInput) (WaitAppStartUsecaseOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, startWait)
	defer cancel()
	for {
		s := u.sessions.Session()
		started := s.Observed.Started
		switch {
		case s.App.State == domain.AppBuildFailed, s.App.State == domain.AppExited && !started:
			return WaitAppStartUsecaseOutput{App: s.App}, nil
		case started || s.App.State == domain.AppExited:
			return WaitAppStartUsecaseOutput{App: s.App, Started: true}, nil
		}
		if err := u.clock.Sleep(ctx, 50*time.Millisecond); err != nil {
			return WaitAppStartUsecaseOutput{}, domain.ErrInternal.Wrap(err, "wait for the app to start")
		}
	}
}
