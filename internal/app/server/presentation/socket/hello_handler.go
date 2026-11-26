package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

type HelloHandler struct {
	usecase *usecase.IngestAppObservationsUsecase
}

func NewHelloHandler(uc *usecase.IngestAppObservationsUsecase) *HelloHandler {
	return &HelloHandler{usecase: uc}
}

func (h *HelloHandler) HandleMessage(ctx context.Context, m agent.Message) error {
	hello, err := agent.Decode[agent.Hello](m)
	if err != nil {
		return domain.ErrInvalidArgument.Wrap(err, "decode hello")
	}
	_, err = h.usecase.Exec(ctx, usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationHello, Hello: domain.AppRun{GoVersion: hello.GoVersion, Procs: hello.Procs, PID: hello.PID}})
	return err
}
