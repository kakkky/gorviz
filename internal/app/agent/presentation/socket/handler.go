package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// RequestHandler は server からの要求 1 つを受け、応答に載せるものを res に書く
type RequestHandler interface {
	HandleRequest(ctx context.Context, req agent.Request, res *domain.Response) error
}

type DisconnectHandler interface {
	HandleDisconnect(ctx context.Context) error
}

// Handler は server からの要求の handler を、usecase から組み立てて並べる
type Handler struct {
	usecase *usecase.UseCase
}

func NewHandler(uc *usecase.UseCase) *Handler {
	return &Handler{usecase: uc}
}

type Route struct {
	Method  agent.Method
	Handler RequestHandler
}

func (h *Handler) Routes() []Route {
	return []Route{
		{
			Method:  agent.MethodResetCoverage,
			Handler: NewResetCoverageHandler(h.usecase.NewResetAppCoverageUsecase()),
		},
		{
			Method:  agent.MethodWriteCoverage,
			Handler: NewWriteCoverageHandler(h.usecase.NewReadAppCoverageUsecase()),
		},
		{
			Method:  agent.MethodWriteGoroutineLeak,
			Handler: NewWriteGoroutineLeakHandler(h.usecase.NewSendAppGoroutineLeaksUsecase()),
		},
		{
			Method:  agent.MethodWriteThreadCount,
			Handler: NewWriteThreadCountHandler(h.usecase.NewWriteAppThreadCountUsecase()),
		},
		{
			Method:  agent.MethodFinish,
			Handler: NewFinishHandler(h.usecase.NewFinishAppObservationUsecase()),
		},
	}
}

// NotFoundHandler は、どの route にも当たらない要求に応答する
func (h *Handler) NotFoundHandler() RequestHandler {
	return notFoundHandler{}
}

func (h *Handler) DisconnectHandler() DisconnectHandler {
	return NewAbandonHandler(h.usecase.NewAbandonAppObservationUsecase())
}

type notFoundHandler struct{}

func (notFoundHandler) HandleRequest(_ context.Context, req agent.Request, _ *domain.Response) error {
	return domain.ErrInvalidArgument.With("unknown method " + string(req.Method))
}
