package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/mcp/domain"
	"github.com/kakkky/gorviz/internal/app/protocol/serverapi"
)

// GetOverviewUsecase は agent の今の様子を取る
type GetOverviewUsecase struct {
	serverFor domain.ServerFor
}

func (us *UseCase) NewGetOverviewUsecase() *GetOverviewUsecase {
	return &GetOverviewUsecase{serverFor: us.serverFor}
}

type GetOverviewUsecaseInput struct {
	Agent string
}

type GetOverviewUsecaseOutput struct {
	Overview serverapi.OverviewJSON
}

func (us *GetOverviewUsecase) Exec(ctx context.Context, in GetOverviewUsecaseInput) (GetOverviewUsecaseOutput, error) {
	overview, err := us.serverFor(in.Agent).Overview(ctx)
	if err != nil {
		return GetOverviewUsecaseOutput{}, err
	}
	return GetOverviewUsecaseOutput{Overview: overview}, nil
}
