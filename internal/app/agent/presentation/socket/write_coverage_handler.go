package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

type WriteCoverageHandler struct {
	usecase *usecase.ReadAppCoverageUsecase
}

func NewWriteCoverageHandler(uc *usecase.ReadAppCoverageUsecase) *WriteCoverageHandler {
	return &WriteCoverageHandler{usecase: uc}
}

func (h *WriteCoverageHandler) HandleRequest(ctx context.Context, _ agent.Request, res *domain.Response) error {
	out, err := h.usecase.Exec(ctx, usecase.ReadAppCoverageUsecaseInput{})
	res.Coverage = out.Files
	return err
}
