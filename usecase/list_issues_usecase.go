package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/domain"
)

type ListIssuesUsecase struct {
	logRecording   domain.LogRecording
	traceRecording domain.TraceRecording
	inMemoryKVS    domain.InMemoryKVS
	policy         domain.IssuePolicy
}

func (us *UseCase) NewListIssuesUsecase() *ListIssuesUsecase {
	return &ListIssuesUsecase{
		logRecording:   us.logRecording,
		traceRecording: us.traceRecording,
		inMemoryKVS:    us.inMemoryKVS,
		policy: domain.IssuePolicy{
			ErrorSpikePerMinute: us.cfg.ErrorSpikePerMinute,
			SlowRequest:         us.cfg.SlowRequest,
			CPUWaitRatio:        us.cfg.CPUWaitRatio,
			GCPerSecond:         us.cfg.GCPerSecond,
		},
	}
}

type ListIssuesUsecaseInput struct {
	Now time.Time
}

type ListIssuesUsecaseOutput struct {
	Issues []domain.Issue
	// ErrorsLastMinute と SlowLastMinute は Overview に出す、直近 1 分のエラーと遅いリクエストの数
	ErrorsLastMinute int
	SlowLastMinute   int
}

// Exec は録画済みのものから Issue を検出する。リークは直近のスナップショット、エラーと遅いリクエストはログ、
// CPU 待ちと GC の頻発は直近の録画の区切りから判定する
func (us *ListIssuesUsecase) Exec(_ context.Context, in ListIssuesUsecaseInput) (ListIssuesUsecaseOutput, error) {
	var issues []domain.Issue
	if snapshot, ok := getAs[RuntimeSnapshot](us.inMemoryKVS, latestSnapshotKey); ok {
		goroutines := getOr(us.inMemoryKVS, latestGoroutinesKey, []domain.Goroutine(nil))
		issues = append(issues, domain.LeakIssues(snapshot.Leaks, goroutines, snapshot.DumpAt)...)
	}
	lines := us.logRecording.Snapshot()
	issues = append(issues, domain.DetectErrorSpikes(lines, in.Now, us.policy, us.locate)...)
	issues = append(issues, domain.DetectSlowRequests(lines, in.Now, us.policy, us.locate)...)
	if c, ok := us.traceRecording.Latest(); ok {
		issues = append(issues, domain.DetectRuntimeIssues(c, us.policy)...)
	}
	domain.SortIssues(issues)
	out := ListIssuesUsecaseOutput{Issues: issues}
	since := in.Now.Add(-time.Minute)
	for _, l := range lines {
		if l.At.Before(since) {
			continue
		}
		if l.Level == domain.LogLevelError {
			out.ErrorsLastMinute++
		}
		if l.HasDuration && l.Duration >= us.policy.SlowRequest {
			out.SlowLastMinute++
		}
	}
	return out, nil
}

// locate はログを書いた G とその瞬間を、slogtrace の番号で trace から正確に決める。番号がなければ G は分からない
func (us *ListIssuesUsecase) locate(l domain.LogLine) (domain.GoroutineID, time.Time, bool) {
	if !l.HasTraceSeq {
		return 0, l.At, false
	}
	c, m, ok := us.traceRecording.FindLogMark(l.TraceSeq)
	if !ok {
		return 0, l.At, false
	}
	return m.Goroutine, c.StartedAt.Add(m.At), true
}

func getAs[T any](kvs domain.InMemoryKVS, key string) (T, bool) {
	var zero T
	v, ok := kvs.Get(key)
	if !ok {
		return zero, false
	}
	t, ok := v.(T)
	return t, ok
}
