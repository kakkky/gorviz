package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/usecase"
)

// AbandonHandler は server との接続が切れたとき、観測をやめる
type AbandonHandler struct {
	usecase *usecase.AbandonAppObservationUsecase
}

func NewAbandonHandler(uc *usecase.AbandonAppObservationUsecase) *AbandonHandler {
	return &AbandonHandler{usecase: uc}
}

func (h *AbandonHandler) HandleDisconnect(ctx context.Context) error {
	_, err := h.usecase.Exec(ctx, usecase.AbandonAppObservationUsecaseInput{})
	return err
}
