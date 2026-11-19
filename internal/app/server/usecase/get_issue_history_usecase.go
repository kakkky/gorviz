package usecase

import (
	"context"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const (
	// issueStepMaxPoints は Issue の推移に並べる区切りの数の上限
	issueStepMaxPoints = 120
	// issueOccurrencesLimit は Issue の詳細に並べる、見つかった回の数の上限 (新しいものから)
	issueOccurrencesLimit = 100
)

// issueSteps は Issue の推移の区切りの長さの候補。同じ Issue は 1 分に 1 回までしか積まないので、1 分より細かくしない
var issueSteps = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour}

type GetIssueHistoryUsecase struct {
	issueStore     domain.IssueStore
	traceRecording domain.TraceRecording
}

func (us *UseCase) NewGetIssueHistoryUsecase() *GetIssueHistoryUsecase {
	return &GetIssueHistoryUsecase{issueStore: us.issueStore, traceRecording: us.traceRecording}
}

// GetIssueHistoryUsecaseInput は Fingerprint の Issue を [To-Span, To] で見る
type GetIssueHistoryUsecaseInput struct {
	Fingerprint string
	To          time.Time
	Span        time.Duration
	Now         time.Time
}

type GetIssueHistoryUsecaseOutput struct {
	From time.Time
	To   time.Time
	Step time.Duration
	// Found は期間の中にこの Issue が見つかっているか。見つかっていなくても、保持していれば Last と FirstSeen は入る
	Found     bool
	Summary   domain.IssueSummary
	Last      domain.IssueOccurrence
	HasLast   bool
	FirstSeen time.Time
	// Steps は区切りごとの、見つかった回数
	Steps []IssueStepCount
	// Occurrences は期間の中で見つかった回 (新しいものから issueOccurrencesLimit 件まで)
	Occurrences []IssueOccurrenceView
}

type IssueStepCount struct {
	At    time.Time
	Count int
}

// IssueOccurrenceView の Replayable は、見つかったときの trace がまだ録画 (保存したものを含む) に残っていて、再生できるか
type IssueOccurrenceView struct {
	domain.IssueOccurrence
	Replayable bool
}

func (us *GetIssueHistoryUsecase) Exec(_ context.Context, in GetIssueHistoryUsecaseInput) (GetIssueHistoryUsecaseOutput, error) {
	if in.Fingerprint == "" {
		return GetIssueHistoryUsecaseOutput{}, model.ErrInvalidArgument.With("no issue is given")
	}
	step := issueStepFor(in.Span)
	from := in.To.Add(-in.Span).Truncate(step)
	out := GetIssueHistoryUsecaseOutput{From: from, To: in.To, Step: step}
	out.Last, out.HasLast = us.issueStore.Last(in.Fingerprint)
	out.FirstSeen, _ = us.issueStore.FirstSeen(in.Fingerprint)

	var occurrences []domain.IssueOccurrence
	for _, o := range us.issueStore.Range(from, in.To) {
		if o.Fingerprint == in.Fingerprint {
			occurrences = append(occurrences, o)
		}
	}
	if s := domain.SummarizeIssues(occurrences, from, in.To, in.Now, 0); len(s) > 0 {
		out.Found, out.Summary = true, s[0]
	}
	for at := from; at.Before(in.To); at = at.Add(step) {
		out.Steps = append(out.Steps, IssueStepCount{At: at})
	}
	for _, o := range occurrences {
		if i := int(o.SeenAt.Sub(from) / step); i >= 0 && i < len(out.Steps) {
			out.Steps[i].Count++
		}
	}
	for _, o := range slices.Backward(occurrences) {
		if len(out.Occurrences) == issueOccurrencesLimit {
			break
		}
		_, replayable := us.traceRecording.At(o.At)
		out.Occurrences = append(out.Occurrences, IssueOccurrenceView{IssueOccurrence: o, Replayable: replayable})
	}
	return out, nil
}

func issueStepFor(span time.Duration) time.Duration {
	for _, s := range issueSteps {
		if span/s <= issueStepMaxPoints {
			return s
		}
	}
	return issueSteps[len(issueSteps)-1]
}
