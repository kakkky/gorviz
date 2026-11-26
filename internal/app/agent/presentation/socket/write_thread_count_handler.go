package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

type WriteThreadCountHandler struct {
	usecase *usecase.WriteAppThreadCountUsecase
}

func NewWriteThreadCountHandler(uc *usecase.WriteAppThreadCountUsecase) *WriteThreadCountHandler {
	return &WriteThreadCountHandler{usecase: uc}
}

func (h *WriteThreadCountHandler) Handle(ctx context.Context, _ agent.Instruction, _ *domain.Reply) error {
	_, err := h.usecase.Exec(ctx, usecase.WriteAppThreadCountUsecaseInput{})
	return err
}
