package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

type TraceHandler struct {
	usecase *usecase.IngestAppObservationsUsecase
}

func NewTraceHandler(uc *usecase.IngestAppObservationsUsecase) *TraceHandler {
	return &TraceHandler{usecase: uc}
}

func (h *TraceHandler) HandleMessage(ctx context.Context, m agent.Message) error {
	_, err := h.usecase.Exec(ctx, usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationTrace, Trace: m.Payload})
	return err
}
