package usecase

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// IssueSummary は Issue の要点。Occurrences は期間の中の発生の回数
type IssueSummary struct {
	Name             string
	Kind             domain.IssueKind
	Subject          string
	Certainty        domain.Certainty
	Occurrences      int
	LastOccurredAt   time.Time
	Ongoing          bool
	OngoingInstances []observation.InstanceName
	NextAction       string
	FirstOccurredAt  time.Time
	// StartedAt は期間の中の発生の始まり。推移を描くのに使う
	StartedAt []time.Time
}

// summarizeIssues は日ごとのファイルを Issue ごとにまとめる。from より前に終わった発生は数えない
func summarizeIssues(days []domain.DatedIssue, from, to time.Time) []IssueSummary {
	byName := map[string]*IssueSummary{}
	for _, d := range days {
		s, ok := byName[d.Issue.Name]
		if !ok {
			s = &IssueSummary{Name: d.Issue.Name, Kind: d.Issue.Kind, Subject: d.Issue.Subject, NextAction: d.Issue.Kind.NextAction()}
			byName[d.Issue.Name] = s
		}
		for _, o := range d.Issue.Occurrences {
			if (!o.Ongoing() && o.EndedAt.Before(from)) || o.StartedAt.After(to) {
				continue
			}
			s.Occurrences++
			s.Certainty = s.Certainty.Stronger(o.Certainty)
			s.LastOccurredAt = maxTime(s.LastOccurredAt, o.StartedAt)
			if s.FirstOccurredAt.IsZero() || o.StartedAt.Before(s.FirstOccurredAt) {
				s.FirstOccurredAt = o.StartedAt
			}
			s.StartedAt = append(s.StartedAt, o.StartedAt)
			if o.Ongoing() {
				s.Ongoing = true
				if !slices.Contains(s.OngoingInstances, o.Instance) {
					s.OngoingInstances = append(s.OngoingInstances, o.Instance)
				}
			}
		}
	}
	out := make([]IssueSummary, 0, len(byName))
	for _, s := range byName {
		if s.Occurrences > 0 {
			slices.SortFunc(s.StartedAt, time.Time.Compare)
			out = append(out, *s)
		}
	}
	slices.SortFunc(out, func(a, b IssueSummary) int {
		return cmp.Or(boolOrder(b.Ongoing, a.Ongoing), b.LastOccurredAt.Compare(a.LastOccurredAt), cmp.Compare(a.Name, b.Name))
	})
	return out
}

func boolOrder(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

type ListIssuesUsecaseInput struct {
	Service  observation.ServiceName
	From     time.Time
	To       time.Time
	Cursor   domain.PageCursor
	MaxBytes int
	// Query と Kind は絞り込み。Query は対象と名前に含まれる文字列 (大文字小文字を区別しない)
	Query string
	Kind  domain.IssueKind
	Now   time.Time
}

type ListIssuesUsecaseOutput struct {
	From          time.Time
	To            time.Time
	Issues        []IssueSummary
	Next          domain.PageCursor
	RetainedSince time.Time
}

type ListIssuesUsecase struct {
	retention domain.Retention
	inventory domain.InventoryAccessor
	issues    domain.IssueAccessor
}

func (us *UseCase) NewListIssuesUsecase() *ListIssuesUsecase {
	return &ListIssuesUsecase{retention: us.settings.Retention, inventory: us.inventory, issues: us.issues}
}

func (u *ListIssuesUsecase) Exec(ctx context.Context, in ListIssuesUsecaseInput) (ListIssuesUsecaseOutput, error) {
	if err := requireService(ctx, u.inventory, in.Service); err != nil {
		return ListIssuesUsecaseOutput{}, err
	}
	after, err := domain.ParsePageCursor(in.Cursor)
	if err != nil {
		return ListIssuesUsecaseOutput{}, err
	}
	since := u.retention.RetainedSince(domain.DataIssues, in.Now)
	from, to := in.From, in.To
	if to.IsZero() {
		to = in.Now
	}
	if from.IsZero() {
		from = since
	}
	// 期間より前に始まって続いている発生も拾うため、ファイルは保持期間の始まりから読む
	days, err := u.issues.ListIssues(ctx, in.Service, since, to)
	if err != nil {
		return ListIssuesUsecaseOutput{}, err
	}
	summaries := slices.DeleteFunc(summarizeIssues(days, from, to), func(s IssueSummary) bool {
		return (in.Kind != "" && s.Kind != in.Kind) || (in.Query != "" && !containsFold(s.Subject+" "+s.Name, in.Query))
	})
	page, next := domain.Page(summaries, after, in.MaxBytes, func(s IssueSummary) int { return len(s.Name) + len(s.Subject) + 160 })
	return ListIssuesUsecaseOutput{From: from, To: to, Issues: page, Next: next, RetainedSince: since}, nil
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
