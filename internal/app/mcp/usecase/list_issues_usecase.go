package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/api/serverapi"
	"github.com/kakkky/gorviz/internal/app/mcp/domain"
)

// ListIssuesUsecase は server が検出した Issue を取る
type ListIssuesUsecase struct {
	serverFor domain.ServerFor
}

func (us *UseCase) NewListIssuesUsecase() *ListIssuesUsecase {
	return &ListIssuesUsecase{serverFor: us.serverFor}
}

type ListIssuesUsecaseInput struct {
	Agent string
}

type ListIssuesUsecaseOutput struct {
	Issues []serverapi.IssueJSON
}

func (us *ListIssuesUsecase) Exec(ctx context.Context, in ListIssuesUsecaseInput) (ListIssuesUsecaseOutput, error) {
	issues, err := us.serverFor(in.Agent).Issues(ctx)
	if err != nil {
		return ListIssuesUsecaseOutput{}, err
	}
	return ListIssuesUsecaseOutput{Issues: issues}, nil
}
