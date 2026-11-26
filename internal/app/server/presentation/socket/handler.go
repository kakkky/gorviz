package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// MessageHandler は agent から届いたメッセージ 1 つを取り込ませる
type MessageHandler interface {
	HandleMessage(ctx context.Context, m agent.Message) error
}

// DisconnectHandler は agent との接続の終わりを受ける。clean は終わりの合図を受けてから切れたこと
type DisconnectHandler interface {
	HandleDisconnect(ctx context.Context, clean bool) error
}

// Handler は agent から届くメッセージの handler を、usecase から組み立てて並べる
type Handler struct {
	usecase *usecase.UseCase
}

func NewHandler(uc *usecase.UseCase) *Handler {
	return &Handler{usecase: uc}
}

type Route struct {
	Type    agent.MessageType
	Handler MessageHandler
}

func (h *Handler) Routes() []Route {
	return []Route{
		{
			Type:    agent.TypeHello,
			Handler: NewHelloHandler(h.usecase.NewIngestAppObservationsUsecase()),
		},
		{
			Type:    agent.TypeTrace,
			Handler: NewTraceHandler(h.usecase.NewIngestAppObservationsUsecase()),
		},
		{
			Type:    agent.TypeGoroutineLeakProfile,
			Handler: NewGoroutineLeakProfileHandler(h.usecase.NewIngestAppObservationsUsecase()),
		},
		{
			Type:    agent.TypeCoverage,
			Handler: NewCoverageHandler(h.usecase.NewIngestAppObservationsUsecase()),
		},
	}
}

func (h *Handler) DisconnectHandler() DisconnectHandler {
	return NewEndHandler(h.usecase.NewIngestAppObservationsUsecase())
}
