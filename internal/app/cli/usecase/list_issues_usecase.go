package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/api/serverapi"
	"github.com/kakkky/gorviz/internal/app/cli/domain"
)

// ListIssuesUsecase は server が検出した Issue を取る
type ListIssuesUsecase struct {
	server domain.Server
}

func (us *UseCase) NewListIssuesUsecase() *ListIssuesUsecase {
	return &ListIssuesUsecase{server: us.server}
}

type ListIssuesUsecaseInput struct{}

type ListIssuesUsecaseOutput struct {
	Issues []serverapi.IssueJSON
}

func (us *ListIssuesUsecase) Exec(ctx context.Context, _ ListIssuesUsecaseInput) (ListIssuesUsecaseOutput, error) {
	issues, err := us.server.Issues(ctx)
	if err != nil {
		return ListIssuesUsecaseOutput{}, err
	}
	return ListIssuesUsecaseOutput{Issues: issues}, nil
}
