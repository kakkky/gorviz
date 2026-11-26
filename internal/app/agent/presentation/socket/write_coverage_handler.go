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

func (h *WriteCoverageHandler) Handle(ctx context.Context, _ agent.Instruction, reply *domain.Reply) error {
	out, err := h.usecase.Exec(ctx, usecase.ReadAppCoverageUsecaseInput{})
	reply.Coverage = out.Files
	return err
}
