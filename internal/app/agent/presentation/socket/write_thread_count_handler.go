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

func (h *WriteThreadCountHandler) HandleRequest(ctx context.Context, _ agent.Request, _ *domain.Response) error {
	_, err := h.usecase.Exec(ctx, usecase.WriteAppThreadCountUsecaseInput{})
	return err
}
