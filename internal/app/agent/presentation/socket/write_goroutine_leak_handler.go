package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

type WriteGoroutineLeakHandler struct {
	usecase *usecase.SendAppGoroutineLeaksUsecase
}

func NewWriteGoroutineLeakHandler(uc *usecase.SendAppGoroutineLeaksUsecase) *WriteGoroutineLeakHandler {
	return &WriteGoroutineLeakHandler{usecase: uc}
}

func (h *WriteGoroutineLeakHandler) HandleRequest(ctx context.Context, _ agent.Request, _ *domain.Response) error {
	_, err := h.usecase.Exec(ctx, usecase.SendAppGoroutineLeaksUsecaseInput{})
	return err
}
