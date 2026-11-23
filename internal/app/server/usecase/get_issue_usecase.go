package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type GetIssueUsecaseInput struct {
	Service observation.ServiceName
	Issue   string
	From    time.Time
	To      time.Time
	Now     time.Time
}

// OccurrenceDetail の TraceRetained は前後の execution trace がまだ保持期間の中にあるか
type OccurrenceDetail struct {
	domain.Occurrence
	TraceRetained bool
}

type DayCount struct {
	Day   time.Time
	Count int
}

type GetIssueUsecaseOutput struct {
	Summary       IssueSummary
	Occurrences   []OccurrenceDetail
	CountsByDay   []DayCount
	RetainedSince time.Time
}

type GetIssueUsecase struct {
	retention domain.Retention
	inventory domain.InventoryAccessor
	issues    domain.IssueAccessor
}

func (us *UseCase) NewGetIssueUsecase() *GetIssueUsecase {
	return &GetIssueUsecase{retention: us.settings.Retention, inventory: us.inventory, issues: us.issues}
}

func (u *GetIssueUsecase) Exec(ctx context.Context, in GetIssueUsecaseInput) (GetIssueUsecaseOutput, error) {
	if err := requireService(ctx, u.inventory, in.Service); err != nil {
		return GetIssueUsecaseOutput{}, err
	}
	since := u.retention.RetainedSince(domain.DataIssues, in.Now)
	from, to := in.From, in.To
	if to.IsZero() {
		to = in.Now
	}
	if from.IsZero() {
		from = since
	}
	days, err := u.issues.ListIssues(ctx, in.Service, since, to)
	if err != nil {
		return GetIssueUsecaseOutput{}, err
	}
	var (
		mine  []domain.DatedIssue
		names []string
		seen  = map[string]bool{}
	)
	for _, d := range days {
		if d.Issue.Name == in.Issue {
			mine = append(mine, d)
		} else if !seen[d.Issue.Name] {
			seen[d.Issue.Name] = true
			names = append(names, d.Issue.Name)
		}
	}
	sums := summarizeIssues(mine, from, to)
	if len(sums) == 0 {
		return GetIssueUsecaseOutput{}, domain.ErrNotFound.WithCandidates("no occurrence of the issue "+in.Issue+" in the period", domain.Candidates(in.Issue, names, candidateLimit))
	}
	out := GetIssueUsecaseOutput{Summary: sums[0], RetainedSince: since}
	traceSince := u.retention.RetainedSince(domain.DataExecutionTracesAroundIssues, in.Now)
	for _, d := range mine {
		n := 0
		for _, o := range d.Issue.Occurrences {
			if (!o.Ongoing() && o.EndedAt.Before(from)) || o.StartedAt.After(to) {
				continue
			}
			n++
			out.Occurrences = append(out.Occurrences, OccurrenceDetail{Occurrence: o, TraceRetained: o.TraceFile != "" && !o.StartedAt.Before(traceSince)})
		}
		out.CountsByDay = append(out.CountsByDay, DayCount{Day: d.Day, Count: n})
	}
	return out, nil
}
