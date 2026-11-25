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

// GetOverviewUsecaseInput の期間がなければ、セッション全体
type GetOverviewUsecaseInput struct {
	From     time.Duration
	To       time.Duration
	HasRange bool
}

// OngoingIssue は、直近の時間幅まで発生が続いている Issue
type OngoingIssue struct {
	ID        domain.IssueID
	Kind      domain.IssueKind
	Target    string
	Certainty domain.Certainty
	Phase     string
	LastAt    time.Duration
}

type PhaseIssues struct {
	Phase  string
	From   time.Duration
	To     time.Duration
	Issues map[domain.IssueKind]int
}

// GetOverviewUsecaseOutput の Buckets は From を含む時間幅から並ぶ
type GetOverviewUsecaseOutput struct {
	Session  domain.Session
	Procs    int
	Elapsed  time.Duration
	From     time.Duration
	To       time.Duration
	Ongoing  []OngoingIssue
	Buckets  []domain.Bucket
	Timeline []domain.SessionEvent
	Phases   []PhaseIssues
	Issues   map[domain.IssueKind]int
}

func (us *UseCase) NewGetOverviewUsecase() *GetOverviewUsecase {
	return &GetOverviewUsecase{sessions: us.sessions, index: us.index, issues: us.issues}
}

// ongoingWindow は Issue の発生が「今も続いている」とみなす、直近の長さ
const ongoingWindow = 2 * domain.BucketSize

func (u *GetOverviewUsecase) Exec(_ context.Context, in GetOverviewUsecaseInput) (GetOverviewUsecaseOutput, error) {
	if in.HasRange && in.To <= in.From {
		return GetOverviewUsecaseOutput{}, domain.ErrInvalidArgument.With("the end of the range must be after its start")
	}
	out := GetOverviewUsecaseOutput{Session: u.sessions.Session(), Issues: map[domain.IssueKind]int{}}
	var phases []domain.PhaseRange
	u.index.ViewTraceIndex(func(ix *domain.TraceIndex) {
		out.Procs, out.Elapsed = ix.Procs(), ix.Last()
		out.From, out.To = 0, ix.Last()
		if in.HasRange {
			out.From, out.To = in.From, min(in.To, ix.Last())
		}
		out.Buckets = append([]domain.Bucket(nil), ix.Buckets(out.From, out.To+1)...)
		out.Timeline = domain.TimelineOf(ix.Marks(), out.Session)
		phases = domain.PhasesOf(ix.Marks(), ix.Last())
	})
	u.issues.ViewIssueBook(func(b *domain.IssueBook) {
		for _, is := range b.Issues() {
			out.Issues[is.Kind]++
			last := is.Occurrences[len(is.Occurrences)-1]
			if out.Session.App.State == domain.AppRunning && last.To >= out.Elapsed-ongoingWindow {
				out.Ongoing = append(out.Ongoing, OngoingIssue{ID: is.ID, Kind: is.Kind, Target: is.TargetLabel(), Certainty: is.Certainty(), Phase: last.Phase, LastAt: last.To})
			}
		}
		for _, p := range phases {
			if p.To < out.From || p.From > out.To {
				continue
			}
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
