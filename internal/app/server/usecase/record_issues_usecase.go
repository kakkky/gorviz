package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// RecordIssuesUsecase は今の判定で見つかった Issue を積み、保存する。続いている Issue は IssueRecordInterval ごとに 1 回だけ積む
type RecordIssuesUsecase struct {
	listIssues *ListIssuesUsecase
	issueStore domain.IssueStore
}

func (us *UseCase) NewRecordIssuesUsecase() *RecordIssuesUsecase {
	return &RecordIssuesUsecase{listIssues: us.NewListIssuesUsecase(), issueStore: us.issueStore}
}

type RecordIssuesUsecaseInput struct {
	Now time.Time
}

type RecordIssuesUsecaseOutput struct {
	Recorded []domain.IssueOccurrence
}

func (us *RecordIssuesUsecase) Exec(ctx context.Context, in RecordIssuesUsecaseInput) (RecordIssuesUsecaseOutput, error) {
	issues, err := us.listIssues.Exec(ctx, ListIssuesUsecaseInput{Now: in.Now})
	if err != nil {
		return RecordIssuesUsecaseOutput{}, err
	}
	var out RecordIssuesUsecaseOutput
	for _, i := range issues.Issues {
		o := domain.NewIssueOccurrence(i, in.Now)
		if last, ok := us.issueStore.Last(o.Fingerprint); ok && in.Now.Sub(last.SeenAt) < domain.IssueRecordInterval {
			continue
		}
		out.Recorded = append(out.Recorded, o)
	}
	us.issueStore.Add(out.Recorded...)
	return out, us.issueStore.Flush(in.Now)
}
