package usecase

import (
	"context"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// DetectAppIssuesUsecase は index に届いた分のうち、まだ判定していない時間幅ごとに Issue を判定する。
// 預けていた goroutineleak プロファイルは、取った時刻の trace が届いていれば goroutine と突き合わせる
type DetectAppIssuesUsecase struct {
	sessions     domain.SessionStore
	observations domain.ObservationAccessor
	issues       domain.IssueBookStore
}

type DetectAppIssuesUsecaseInput struct{}

type DetectAppIssuesUsecaseOutput struct {
	DetectedUntil time.Duration
	PendingLeaks  int
}

func (us *UseCase) NewDetectAppIssuesUsecase() *DetectAppIssuesUsecase {
	return &DetectAppIssuesUsecase{sessions: us.sessions, observations: us.observations, issues: us.issues}
}

type phasedDetection struct {
	d     domain.Detection
	phase string
}

func (u *DetectAppIssuesUsecase) Exec(_ context.Context, _ DetectAppIssuesUsecaseInput) (DetectAppIssuesUsecaseOutput, error) {
	var until time.Duration
	var pending []domain.PendingLeakCheck
	u.issues.ViewIssueBook(func(b *domain.IssueBook) {
		until = b.DetectedUntil
		pending = append(pending, b.Pending...)
	})
	var found []phasedDetection
	var done []time.Duration
	var next time.Duration
	observed := u.sessions.Session().Observed
	phases := domain.PhasesOf(observed.Marks, observed.Until)
	next = until
	for from := until; from < observed.Until; from += domain.BucketSize {
		to := min(from+domain.BucketSize, observed.Until)
		set, err := observationSet(u.observations, u.sessions, from, to)
		if err != nil {
			return DetectAppIssuesUsecaseOutput{}, err
		}
		for _, d := range domain.DetectRuntimeIssues(domain.NewWindowStats(set, from, to)) {
			found = append(found, phasedDetection{d: d, phase: domain.PhaseAt(phases, from)})
		}
		next = to
	}
	for _, p := range pending {
		if p.At >= observed.Until && !observed.Ended {
			continue
		}
		done = append(done, p.At)
		// trace の末尾は欠けるので、最後の区切りより後に取ったプロファイルは、最後の区切りの状態と突き合わせ、trace の最後の phase に入れる
		at := min(p.At, max(0, observed.Until-1))
		set, err := observationSet(u.observations, u.sessions, at, at)
		if err != nil {
			return DetectAppIssuesUsecaseOutput{}, err
		}
		phase := domain.PhaseAt(phases, min(p.At, observed.Until))
		for _, d := range domain.MatchLeaks(set, at, p.Stacks) {
			found = append(found, phasedDetection{d: d, phase: phase})
		}
	}
	left := 0
	u.issues.UpdateIssueBook(func(b *domain.IssueBook) {
		for _, f := range found {
			b.Record(f.d, f.phase)
		}
		b.DetectedUntil = max(b.DetectedUntil, next)
		var rest []domain.PendingLeakCheck
		for _, p := range b.Pending {
			if !slices.Contains(done, p.At) {
				rest = append(rest, p)
			}
		}
		b.Pending = rest
		left = len(rest)
	})
	return DetectAppIssuesUsecaseOutput{DetectedUntil: next, PendingLeaks: left}, nil
}
