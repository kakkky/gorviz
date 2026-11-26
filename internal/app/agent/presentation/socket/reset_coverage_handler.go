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

func (h *ResetCoverageHandler) HandleRequest(ctx context.Context, _ agent.Request, _ *domain.Response) error {
	_, err := h.usecase.Exec(ctx, usecase.ResetAppCoverageUsecaseInput{})
	return err
}
