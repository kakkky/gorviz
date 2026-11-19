package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// issueTrendBuckets は Issues の一覧に出す推移の区切りの数
const issueTrendBuckets = 30

type ListIssueHistoryUsecase struct {
	issueStore domain.IssueStore
}

func (us *UseCase) NewListIssueHistoryUsecase() *ListIssueHistoryUsecase {
	return &ListIssueHistoryUsecase{issueStore: us.issueStore}
}

// ListIssueHistoryUsecaseInput は [To-Span, To] に見つかった Issue を、Query (文字) と Kind (空ならすべて) で絞る。
// To が空なら Now まで。今も起きているか (active) は Now で決める
type ListIssueHistoryUsecaseInput struct {
	Now   time.Time
	To    time.Time
	Span  time.Duration
	Query string
	Kind  model.IssueKind
}

type ListIssueHistoryUsecaseOutput struct {
	From   time.Time
	To     time.Time
	Issues []IssueHistory
	// Step と Steps は期間の推移 (区切りごとの、種類別の見つかった回数)。Totals は期間の中の種類別の回数
	Step   time.Duration
	Steps  []IssueKindStepCount
	Totals map[model.IssueKind]int
}

type IssueKindStepCount struct {
	At     time.Time
	Counts map[model.IssueKind]int
}

// IssueHistory は期間の中の 1 つの Issue と、保持しているうちで最初に見つけた時刻
type IssueHistory struct {
	domain.IssueSummary
	FirstSeen time.Time
}

func (us *ListIssueHistoryUsecase) Exec(_ context.Context, in ListIssueHistoryUsecaseInput) (ListIssueHistoryUsecaseOutput, error) {
	to := in.To
	if to.IsZero() {
		to = in.Now
	}
	step := issueStepFor(in.Span)
	// 区切りの境目を時刻の切りのよいところに揃え、refresh で期間が進んでも区切りがずれないようにする
	from := to.Add(-in.Span).Truncate(step)
	var occurrences []domain.IssueOccurrence
	for _, o := range us.issueStore.Range(from, to) {
		if (in.Kind == "" || o.Kind == in.Kind) && domain.MatchesIssueQuery(o, in.Query) {
			occurrences = append(occurrences, o)
		}
	}
	out := ListIssueHistoryUsecaseOutput{From: from, To: to, Step: step, Totals: map[model.IssueKind]int{}}
	for at := from; at.Before(to); at = at.Add(step) {
		out.Steps = append(out.Steps, IssueKindStepCount{At: at, Counts: map[model.IssueKind]int{}})
	}
	for _, o := range occurrences {
		out.Totals[o.Kind]++
		if i := int(o.SeenAt.Sub(from) / step); i >= 0 && i < len(out.Steps) {
			out.Steps[i].Counts[o.Kind]++
		}
	}
	for _, s := range domain.SummarizeIssues(occurrences, from, to, in.Now, issueTrendBuckets) {
		h := IssueHistory{IssueSummary: s}
		h.FirstSeen, _ = us.issueStore.FirstSeen(s.Latest.Fingerprint)
		out.Issues = append(out.Issues, h)
	}
	return out, nil
}
