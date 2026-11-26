package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

type GoroutineLeakProfileHandler struct {
	usecase *usecase.IngestAppObservationsUsecase
}

func NewGoroutineLeakProfileHandler(uc *usecase.IngestAppObservationsUsecase) *GoroutineLeakProfileHandler {
	return &GoroutineLeakProfileHandler{usecase: uc}
}

func (h *GoroutineLeakProfileHandler) HandleMessage(ctx context.Context, m agent.Message) error {
	p, err := agent.Decode[agent.GoroutineLeakProfile](m)
	if err != nil {
		return domain.ErrInvalidArgument.Wrap(err, "decode goroutineleak profile")
	}
	_, err = h.usecase.Exec(ctx, usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationGoroutineLeakProfile, LeakSeq: p.Seq, LeakText: p.Text})
	return err
}
