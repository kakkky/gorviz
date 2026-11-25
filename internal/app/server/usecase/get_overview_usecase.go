package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// GetOverviewUsecase はセッション全体の様子 (phase ごとの Issue の数と、時間幅ごとの値の推移) を返す
type GetOverviewUsecase struct {
	sessions domain.SessionStore
	index    domain.TraceIndexStore
	issues   domain.IssueBookStore
}

type GetOverviewUsecaseInput struct{}

type PhaseIssues struct {
	Phase  string
	From   time.Duration
	To     time.Duration
	Issues map[domain.IssueKind]int
}

type GetOverviewUsecaseOutput struct {
	Session  domain.Session
	Procs    int
	Elapsed  time.Duration
	Buckets  []domain.Bucket
	Timeline []domain.SessionEvent
	Phases   []PhaseIssues
	Issues   map[domain.IssueKind]int
}

func (us *UseCase) NewGetOverviewUsecase() *GetOverviewUsecase {
	return &GetOverviewUsecase{sessions: us.sessions, index: us.index, issues: us.issues}
}

func (u *GetOverviewUsecase) Exec(_ context.Context, _ GetOverviewUsecaseInput) (GetOverviewUsecaseOutput, error) {
	out := GetOverviewUsecaseOutput{Session: u.sessions.Session(), Issues: map[domain.IssueKind]int{}}
	var phases []domain.PhaseRange
	u.index.ViewTraceIndex(func(ix *domain.TraceIndex) {
		out.Procs, out.Elapsed = ix.Procs(), ix.Last()
		out.Buckets = append([]domain.Bucket(nil), ix.Buckets(0, ix.Last()+domain.BucketSize)...)
		out.Timeline = domain.TimelineOf(ix.Marks(), out.Session)
		phases = domain.PhasesOf(ix.Marks(), ix.Last())
	})
	u.issues.ViewIssueBook(func(b *domain.IssueBook) {
		for _, is := range b.Issues() {
			out.Issues[is.Kind]++
		}
		for _, p := range phases {
			pi := PhaseIssues{Phase: p.Name, From: p.From, To: p.To, Issues: map[domain.IssueKind]int{}}
			for _, is := range b.Issues() {
				for _, o := range is.Occurrences {
					if o.Phase == p.Name && o.From >= p.From && o.From <= p.To {
						pi.Issues[is.Kind]++
						break
					}
				}
			}
			out.Phases = append(out.Phases, pi)
		}
	})
	return out, nil
}
