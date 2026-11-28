package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// GetSessionStatusUsecase はセッションの今の状態 (アプリが動いているか・診断した時間帯・時間軸の出来事) を返す
type GetSessionStatusUsecase struct {
	sessions domain.SessionStore
	issues   domain.IssueBookStore
}

type GetSessionStatusUsecaseInput struct{}

type GetSessionStatusUsecaseOutput struct {
	Session  domain.Session
	Elapsed  time.Duration
	Timeline []domain.SessionEvent
	Issues   int
}

func (us *UseCase) NewGetSessionStatusUsecase() *GetSessionStatusUsecase {
	return &GetSessionStatusUsecase{sessions: us.sessions, issues: us.issues}
}

func (u *GetSessionStatusUsecase) Exec(_ context.Context, _ GetSessionStatusUsecaseInput) (GetSessionStatusUsecaseOutput, error) {
	out := GetSessionStatusUsecaseOutput{Session: u.sessions.Session()}
	out.Elapsed = out.Session.Observed.Until
	out.Timeline = domain.TimelineOf(out.Session.Observed.Marks, out.Session)
	u.issues.ViewIssueBook(func(b *domain.IssueBook) { out.Issues = len(b.Issues()) })
	return out, nil
}
