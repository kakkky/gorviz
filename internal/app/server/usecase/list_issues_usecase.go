package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

type ListIssuesUsecase struct {
	traceRecording domain.TraceRecording
	inMemoryKVS    domain.InMemoryKVS
	policy         domain.IssuePolicy
}

func (us *UseCase) NewListIssuesUsecase() *ListIssuesUsecase {
	return &ListIssuesUsecase{
		traceRecording: us.traceRecording,
		inMemoryKVS:    us.inMemoryKVS,
		policy:         us.settings.Issue,
	}
}

type ListIssuesUsecaseInput struct {
	Now time.Time
}

type ListIssuesUsecaseOutput struct {
	Issues []domain.Issue
}

// Exec は録画済みのものから Issue を検出する。リークは直近のスナップショット、CPU 待ちと GC の頻発は直近の録画の区切りから判定する。
// 遅いリクエストやエラーの増加のようなアプリ層の異常は APM (Datadog など) の役目なので、ここでは判定しない
func (us *ListIssuesUsecase) Exec(_ context.Context, _ ListIssuesUsecaseInput) (ListIssuesUsecaseOutput, error) {
	var issues []domain.Issue
	if snapshot, ok := domain.KVSGet[RuntimeSnapshot](us.inMemoryKVS, latestSnapshotKey); ok {
		goroutines := domain.KVSGetOr(us.inMemoryKVS, latestGoroutinesKey, []model.Goroutine(nil))
		issues = append(issues, domain.LeakIssues(snapshot.Leaks, goroutines, snapshot.DumpAt)...)
	}
	if c, ok := us.traceRecording.Latest(); ok {
		issues = append(issues, domain.DetectRuntimeIssues(c, us.policy)...)
	}
	domain.SortIssues(issues)
	return ListIssuesUsecaseOutput{Issues: issues}, nil
}
