package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

type ResetCoverageHandler struct {
	usecase *usecase.ResetAppCoverageUsecase
}

func NewResetCoverageHandler(uc *usecase.ResetAppCoverageUsecase) *ResetCoverageHandler {
	return &ResetCoverageHandler{usecase: uc}
}

func (h *ResetCoverageHandler) Handle(ctx context.Context, _ agent.Instruction, _ *domain.Reply) error {
	_, err := h.usecase.Exec(ctx, usecase.ResetAppCoverageUsecaseInput{})
	return err
}
