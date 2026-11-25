package usecase

import "github.com/kakkky/gorviz/internal/app/server/domain"

type UseCase struct {
	sessions  domain.SessionStore
	index     domain.TraceIndexStore
	issues    domain.IssueBookStore
	stream    domain.TraceStream
	agent     domain.AgentInstructor
	builder   domain.AppBuilder
	starter   domain.AppStarter
	requester domain.AppRequester
	toolchain domain.Toolchain
	sources   domain.AgentSources
	clock     domain.Clock
}

func NewUseCase(
	sessions domain.SessionStore,
	index domain.TraceIndexStore,
	issues domain.IssueBookStore,
	stream domain.TraceStream,
	agent domain.AgentInstructor,
	builder domain.AppBuilder,
	starter domain.AppStarter,
	requester domain.AppRequester,
	toolchain domain.Toolchain,
	sources domain.AgentSources,
	clock domain.Clock,
) *UseCase {
	return &UseCase{sessions: sessions, index: index, issues: issues, stream: stream, agent: agent, builder: builder, starter: starter, requester: requester, toolchain: toolchain, sources: sources, clock: clock}
}
