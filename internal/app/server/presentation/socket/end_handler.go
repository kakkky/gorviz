package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// EndHandler は agent との接続が切れたら trace を閉じる。終わりの合図なしに切れたら、壊れたものとして閉じる
type EndHandler struct {
	usecase *usecase.IngestAppObservationsUsecase
}

func NewEndHandler(uc *usecase.IngestAppObservationsUsecase) *EndHandler {
	return &EndHandler{usecase: uc}
}

func (h *EndHandler) HandleDisconnect(ctx context.Context, clean bool) error {
	_, err := h.usecase.Exec(ctx, usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationEnd, Broken: !clean})
	return err
}
