package usecase

import "github.com/kakkky/gorviz/internal/app/server/domain"

type UseCase struct {
	sessions     domain.SessionStore
	observations domain.ObservationAccessor
	issues       domain.IssueBookStore
	stream       domain.TraceStream
	builder      domain.AppBuilder
	starter      domain.AppStarter
	profiler     domain.AppProfiler
	requester    domain.AppRequester
	clock        domain.Clock
}

func NewUseCase(
	sessions domain.SessionStore,
	observations domain.ObservationAccessor,
	issues domain.IssueBookStore,
	stream domain.TraceStream,
	builder domain.AppBuilder,
	starter domain.AppStarter,
	profiler domain.AppProfiler,
	requester domain.AppRequester,
	clock domain.Clock,
) *UseCase {
	return &UseCase{sessions: sessions, observations: observations, issues: issues, stream: stream, builder: builder, starter: starter, profiler: profiler, requester: requester, clock: clock}
}
