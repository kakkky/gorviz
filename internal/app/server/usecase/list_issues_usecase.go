package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// ListIssuesUsecase はセッションで見つけた Issue の要点を返す。中身は GetIssueUsecase で返す
type ListIssuesUsecase struct {
	issues domain.IssueBookStore
}

// ListIssuesUsecaseInput の Kind・Text が空でなければ、その種類・対象を含むものだけを返す
type ListIssuesUsecaseInput struct {
	Kind domain.IssueKind
	Text string
}

type IssueSummary struct {
	ID          domain.IssueID
	Kind        domain.IssueKind
	Target      string
	Certainty   domain.Certainty
	Occurrences int
	FirstPhase  string
	FirstAt     time.Duration
	LastAt      time.Duration
	Value       string
	Starts      []time.Duration
}

type ListIssuesUsecaseOutput struct {
	Issues []IssueSummary
}

func (us *UseCase) NewListIssuesUsecase() *ListIssuesUsecase {
	return &ListIssuesUsecase{issues: us.issues}
}

func (u *ListIssuesUsecase) Exec(_ context.Context, in ListIssuesUsecaseInput) (ListIssuesUsecaseOutput, error) {
	var out ListIssuesUsecaseOutput
	u.issues.ViewIssueBook(func(b *domain.IssueBook) {
		for _, is := range b.Issues() {
			if in.Kind != "" && is.Kind != in.Kind {
				continue
			}
			if in.Text != "" && !strings.Contains(strings.ToLower(is.TargetLabel()+" "+string(is.Kind)), strings.ToLower(in.Text)) {
				continue
			}
			first, last := is.Occurrences[0], is.Occurrences[len(is.Occurrences)-1]
			out.Issues = append(out.Issues, IssueSummary{ID: is.ID, Kind: is.Kind, Target: is.TargetLabel(), Certainty: is.Certainty(), Occurrences: len(is.Occurrences),
				FirstPhase: is.FirstPhase(), FirstAt: first.From, LastAt: last.To, Value: last.Value, Starts: starts(is.Occurrences)})
		}
	})
	return out, nil
}

func starts(os []domain.Occurrence) []time.Duration {
	out := make([]time.Duration, 0, len(os))
	for _, o := range os {
		out = append(out, o.From)
	}
	return out
}
