package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// CollectAppObservationsUsecase は、要るときにアプリの pprof から goroutineleak プロファイルを取る。
// 今回取るかは、待ちの goroutine の数と shutdown の最中かから domain が決める
type CollectAppObservationsUsecase struct {
	sessions     domain.SessionStore
	observations domain.ObservationAccessor
	issues       domain.IssueBookStore
	profiler     domain.AppProfiler
	clock        domain.Clock
}

type CollectAppObservationsUsecaseInput struct{}

// CollectAppObservationsUsecaseOutput の AppGone は、アプリがもう動いていないこと (ビルドや起動に失敗したときを含む)
type CollectAppObservationsUsecaseOutput struct {
	TookLeaks bool
	AppGone   bool
}

func (us *UseCase) NewCollectAppObservationsUsecase() *CollectAppObservationsUsecase {
	return &CollectAppObservationsUsecase{sessions: us.sessions, observations: us.observations, issues: us.issues, profiler: us.profiler, clock: us.clock}
}

func (u *CollectAppObservationsUsecase) Exec(ctx context.Context, _ CollectAppObservationsUsecaseInput) (CollectAppObservationsUsecaseOutput, error) {
	s := u.sessions.Session()
	if appGone(s.App) || s.App.State == domain.AppExited {
		return CollectAppObservationsUsecaseOutput{AppGone: true}, nil
	}
	if s.App.State != domain.AppRunning || !s.Observed.Started {
		return CollectAppObservationsUsecaseOutput{}, nil
	}
	now := u.clock.Now()
	waiting := 0
	if o, ok := u.observations.Get(int(s.Observed.Until/domain.BucketSize) - 1); ok {
		waiting = o.Stats.Waiting
	}
	if !domain.LeaksDue(s.Collected, now, waiting, s.Observed.ShuttingDown()) {
		return CollectAppObservationsUsecaseOutput{}, nil
	}
	ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := takeGoroutineLeaks(ictx, u.profiler, u.sessions, u.issues, now); err != nil {
		return CollectAppObservationsUsecaseOutput{}, err
	}
	u.sessions.UpdateSession(func(s *domain.Session) { s.Collected = domain.Collected{Leaks: now, LeakWaiting: waiting} })
	return CollectAppObservationsUsecaseOutput{TookLeaks: true}, nil
}
