package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

type WriteTraceEventHandler struct {
	usecase *usecase.WriteAppRuntimeTraceEventUsecase
}

func NewWriteTraceEventHandler(uc *usecase.WriteAppRuntimeTraceEventUsecase) *WriteTraceEventHandler {
	return &WriteTraceEventHandler{usecase: uc}
}

func (h *WriteTraceEventHandler) Handle(ctx context.Context, in agent.Instruction, _ *domain.Reply) error {
	_, err := h.usecase.Exec(ctx, usecase.WriteAppRuntimeTraceEventUsecaseInput{Event: in.Event})
	return err
}
