package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// LiveWindow は期間を指定せずに再生を開いたときに追う、直近の長さ
const LiveWindow = 2 * time.Second

// GetSchedulerWindowUsecase はスケジューラの動きを再生する期間の材料を返す。期間がなければ直近 LiveWindow を返す
type GetSchedulerWindowUsecase struct {
	sessions     domain.SessionStore
	observations domain.ObservationAccessor
	issues       domain.IssueBookStore
}

type GetSchedulerWindowUsecaseInput struct {
	From     time.Duration
	To       time.Duration
	HasRange bool
}

type GetSchedulerWindowUsecaseOutput struct {
	Window domain.SchedulerWindow
	Leaks  []domain.LeakStack
}

func (us *UseCase) NewGetSchedulerWindowUsecase() *GetSchedulerWindowUsecase {
	return &GetSchedulerWindowUsecase{sessions: us.sessions, observations: us.observations, issues: us.issues}
}

func (u *GetSchedulerWindowUsecase) Exec(_ context.Context, in GetSchedulerWindowUsecaseInput) (GetSchedulerWindowUsecaseOutput, error) {
	if in.HasRange && in.To <= in.From {
		return GetSchedulerWindowUsecaseOutput{}, domain.ErrInvalidArgument.With("the end of the range must be after its start")
	}
	leaks := (&GetGoroutineDetailUsecase{issues: u.issues}).leaks()
	from, to := in.From, in.To
	if !in.HasRange {
		to = u.sessions.Session().Observed.Until
		from = max(0, to-LiveWindow)
	}
	set, err := observationSet(u.observations, u.sessions, from, to)
	if err != nil {
		return GetSchedulerWindowUsecaseOutput{}, err
	}
	out := GetSchedulerWindowUsecaseOutput{Window: domain.NewSchedulerWindow(set, from, to), Leaks: leaks}
	out.Window.Live = !in.HasRange
	return out, nil
}
