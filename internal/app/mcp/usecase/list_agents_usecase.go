package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/mcp/domain"
	"github.com/kakkky/gorviz/internal/app/protocol/serverapi"
)

// ListAgentsUsecase は agent の一覧を取る
type ListAgentsUsecase struct {
	serverFor domain.ServerFor
}

func (us *UseCase) NewListAgentsUsecase() *ListAgentsUsecase {
	return &ListAgentsUsecase{serverFor: us.serverFor}
}

type ListAgentsUsecaseInput struct {
}

type ListAgentsUsecaseOutput struct {
	Agents []serverapi.AgentJSON
}

func (us *ListAgentsUsecase) Exec(ctx context.Context, _ ListAgentsUsecaseInput) (ListAgentsUsecaseOutput, error) {
	agents, err := us.serverFor("").Agents(ctx)
	if err != nil {
		return ListAgentsUsecaseOutput{}, err
	}
	return ListAgentsUsecaseOutput{Agents: agents}, nil
}
