package usecase

import "github.com/kakkky/gorviz/internal/app/server/domain"

type UseCase struct {
	sessions     domain.SessionStore
	observations domain.ObservationAccessor
	issues       domain.IssueBookStore
	stream       domain.TraceStream
	agent        domain.AgentInstructor
	builder      domain.AppBuilder
	starter      domain.AppStarter
	requester    domain.AppRequester
	toolchain    domain.Toolchain
	clock        domain.Clock
}

func NewUseCase(
	sessions domain.SessionStore,
	observations domain.ObservationAccessor,
	issues domain.IssueBookStore,
	stream domain.TraceStream,
	agent domain.AgentInstructor,
	builder domain.AppBuilder,
	starter domain.AppStarter,
	requester domain.AppRequester,
	toolchain domain.Toolchain,
	clock domain.Clock,
) *UseCase {
	return &UseCase{sessions: sessions, observations: observations, issues: issues, stream: stream, agent: agent, builder: builder, starter: starter, requester: requester, toolchain: toolchain, clock: clock}
}
