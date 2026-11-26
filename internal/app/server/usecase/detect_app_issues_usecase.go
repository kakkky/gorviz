package usecase

import (
	"context"
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
	var done []uint64
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
		at, ok := observed.LeakCheckAt(p.Seq)
		if !ok && !observed.Ended {
			continue
		}
		if ok && at >= observed.Until && !observed.Ended {
			continue
		}
		done = append(done, p.Seq)
		if !ok {
			continue
		}
		set, err := observationSet(u.observations, u.sessions, at, at)
		if err != nil {
			return DetectAppIssuesUsecaseOutput{}, err
		}
		for _, d := range domain.MatchLeaks(set, at, p.Stacks) {
			found = append(found, phasedDetection{d: d, phase: domain.PhaseAt(phases, at)})
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
			if !containsSeq(done, p.Seq) {
				rest = append(rest, p)
			}
		}
		b.Pending = rest
		left = len(rest)
	})
	return DetectAppIssuesUsecaseOutput{DetectedUntil: next, PendingLeaks: left}, nil
}

func containsSeq(seqs []uint64, s uint64) bool {
	for _, v := range seqs {
		if v == s {
			return true
		}
	}
	return false
}
