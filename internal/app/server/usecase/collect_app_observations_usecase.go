package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// CollectAppObservationsUsecase は agent に、OS のスレッドの数と goroutineleak プロファイルを書き出させる。
// どちらを今回行うかは domain が決める。goroutineleak プロファイルは取るたびに GC が走るので、間隔を空ける
type CollectAppObservationsUsecase struct {
	sessions domain.SessionStore
	agent    domain.AgentInstructor
	clock    domain.Clock
}

type CollectAppObservationsUsecaseInput struct{}

type CollectAppObservationsUsecaseOutput struct{}

func (us *UseCase) NewCollectAppObservationsUsecase() *CollectAppObservationsUsecase {
	return &CollectAppObservationsUsecase{sessions: us.sessions, agent: us.agent, clock: us.clock}
}

func (u *CollectAppObservationsUsecase) Exec(ctx context.Context, _ CollectAppObservationsUsecaseInput) (CollectAppObservationsUsecaseOutput, error) {
	s := u.sessions.Session()
	if s.App.State != domain.AppRunning || !u.agent.Connected() {
		return CollectAppObservationsUsecaseOutput{}, nil
	}
	now := u.clock.Now()
	due := domain.DueCollections(s.Collected, now)
	ictx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if due.Threads {
		if err := u.agent.RequestThreadCount(ictx); err != nil {
			return CollectAppObservationsUsecaseOutput{}, err
		}
	}
	if due.Leaks {
		if err := u.agent.RequestGoroutineLeaks(ictx); err != nil {
			return CollectAppObservationsUsecaseOutput{}, err
		}
	}
	u.sessions.UpdateSession(func(s *domain.Session) { s.Collected = domain.MarkCollected(s.Collected, due, now) })
	return CollectAppObservationsUsecaseOutput{}, nil
}
