package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/api/serverapi"
	"github.com/kakkky/gorviz/internal/app/mcp/domain"
)

// SearchLogsUsecase はログを絞り込んで取る
type SearchLogsUsecase struct {
	serverFor domain.ServerFor
}

func (us *UseCase) NewSearchLogsUsecase() *SearchLogsUsecase {
	return &SearchLogsUsecase{serverFor: us.serverFor}
}

type SearchLogsUsecaseInput struct {
	Agent string
	Query serverapi.LogQuery
}

type SearchLogsUsecaseOutput struct {
	Logs []serverapi.LogJSON
}

func (us *SearchLogsUsecase) Exec(ctx context.Context, in SearchLogsUsecaseInput) (SearchLogsUsecaseOutput, error) {
	logs, err := us.serverFor(in.Agent).Logs(ctx, in.Query)
	if err != nil {
		return SearchLogsUsecaseOutput{}, err
	}
	return SearchLogsUsecaseOutput{Logs: logs}, nil
}
