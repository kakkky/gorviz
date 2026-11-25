package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// GetSessionStatusUsecase はセッションの今の状態 (アプリが動いているか・診断した時間帯・時間軸の出来事) を返す
type GetSessionStatusUsecase struct {
	sessions domain.SessionStore
	index    domain.TraceIndexStore
	issues   domain.IssueBookStore
	agent    domain.AgentInstructor
}

type GetSessionStatusUsecaseInput struct{}

type GetSessionStatusUsecaseOutput struct {
	Session        domain.Session
	AgentConnected bool
	Elapsed        time.Duration
	Timeline       []domain.SessionEvent
	Issues         int
}

func (us *UseCase) NewGetSessionStatusUsecase() *GetSessionStatusUsecase {
	return &GetSessionStatusUsecase{sessions: us.sessions, index: us.index, issues: us.issues, agent: us.agent}
}

func (u *GetSessionStatusUsecase) Exec(_ context.Context, _ GetSessionStatusUsecaseInput) (GetSessionStatusUsecaseOutput, error) {
	out := GetSessionStatusUsecaseOutput{Session: u.sessions.Session(), AgentConnected: u.agent.Connected()}
	u.index.ViewTraceIndex(func(ix *domain.TraceIndex) {
		out.Elapsed = ix.Last()
		out.Timeline = domain.TimelineOf(ix.Marks(), out.Session)
	})
	u.issues.ViewIssueBook(func(b *domain.IssueBook) { out.Issues = len(b.Issues()) })
	return out, nil
}
