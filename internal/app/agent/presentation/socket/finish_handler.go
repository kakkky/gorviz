package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/usecase"
)

type FinishHandler struct {
	usecase *usecase.FinishAppObservationUsecase
}

func NewFinishHandler(uc *usecase.FinishAppObservationUsecase) *FinishHandler {
	return &FinishHandler{usecase: uc}
}

func (h *FinishHandler) Handle(ctx context.Context) error {
	_, err := h.usecase.Exec(ctx, usecase.FinishAppObservationUsecaseInput{})
	return err
}
