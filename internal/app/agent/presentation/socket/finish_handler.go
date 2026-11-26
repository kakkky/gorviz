package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// FinishHandler は最後の観測データを送って接続を閉じる。そのあとに書く応答は届かないが、server も待たない
type FinishHandler struct {
	usecase *usecase.FinishAppObservationUsecase
}

func NewFinishHandler(uc *usecase.FinishAppObservationUsecase) *FinishHandler {
	return &FinishHandler{usecase: uc}
}

func (h *FinishHandler) HandleRequest(ctx context.Context, _ agent.Request, _ *domain.Response) error {
	_, err := h.usecase.Exec(ctx, usecase.FinishAppObservationUsecaseInput{})
	return err
}
