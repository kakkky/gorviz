package usecase

import (
	"context"
	"fmt"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// GetIssueUsecase は Issue の発生の時間軸と、1 つの発生 (指定がなければ最新) で影響の大きかった goroutine の詳細を返す
type GetIssueUsecase struct {
	index  domain.TraceIndexStore
	issues domain.IssueBookStore
}

// GetIssueUsecaseInput の Occurrence は 1 から数える。0 なら最新
type GetIssueUsecaseInput struct {
	ID         domain.IssueID
	Occurrence int
}

type GetIssueUsecaseOutput struct {
	Issue      domain.Issue
	Certainty  domain.Certainty
	Occurrence int
	Impact     domain.Impact
	NextAction string
	Phases     []domain.PhaseRange
}

func (us *UseCase) NewGetIssueUsecase() *GetIssueUsecase {
	return &GetIssueUsecase{index: us.index, issues: us.issues}
}

func (u *GetIssueUsecase) Exec(_ context.Context, in GetIssueUsecaseInput) (GetIssueUsecaseOutput, error) {
	var is domain.Issue
	var err error
	u.issues.ViewIssueBook(func(b *domain.IssueBook) {
		found, ok := b.Issue(in.ID)
		if !ok {
			err = domain.IssueNotFound(in.ID, b.Issues())
			return
		}
		is = *found
		is.Occurrences = append([]domain.Occurrence(nil), found.Occurrences...)
	})
	if err != nil {
		return GetIssueUsecaseOutput{}, err
	}
	n := in.Occurrence
	if n == 0 {
		n = len(is.Occurrences)
	}
	o, ok := is.Occurrence(n)
	if !ok {
		return GetIssueUsecaseOutput{}, domain.ErrNotFound.With(fmt.Sprintf("issue %s has occurrences 1 to %d", is.ID, len(is.Occurrences)))
	}
	out := GetIssueUsecaseOutput{Issue: is, Certainty: is.Certainty(), Occurrence: n, NextAction: is.Kind.NextAction()}
	u.index.ViewTraceIndex(func(ix *domain.TraceIndex) {
		out.Impact = domain.ImpactOf(ix, &is, o)
		out.Phases = domain.PhasesOf(ix.Marks(), ix.Last())
	})
	return out, nil
}
