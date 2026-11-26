package api

import (
	"context"
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/server"
)

// Handler は cli に返す HTTP API の handler を、usecase から組み立てて並べる
type Handler struct {
	usecase *usecase.UseCase
	guiBase string
	stop    context.CancelFunc
}

// NewHandler の stop は、server を止める。アプリの起動に失敗したときと、gorviz stop で呼ぶ
func NewHandler(uc *usecase.UseCase, guiBase string, stop context.CancelFunc) *Handler {
	return &Handler{usecase: uc, guiBase: guiBase, stop: stop}
}

type Route struct {
	Pattern string
	Handler http.Handler
}

func (h *Handler) Routes() []Route {
	return []Route{
		{
			Pattern: "GET " + server.StatusEndpoint,
			Handler: NewGetStatusHandler(h.usecase.NewGetSessionStatusUsecase(), h.guiBase),
		},
		{
			Pattern: "GET " + server.StartResultEndpoint,
			Handler: NewGetStartResultHandler(h.usecase.NewWaitAppStartUsecase(), h.stop, h.guiBase),
		},
		{
			Pattern: "POST " + server.DiagnoseEndpoint,
			Handler: NewPostDiagnoseHandler(h.usecase.NewDiagnoseAppUsecase(), h.guiBase),
		},
		{
			Pattern: "GET " + server.IssuesEndpoint,
			Handler: NewGetIssuesHandler(h.usecase.NewListIssuesUsecase(), h.guiBase),
		},
		{
			Pattern: "GET " + server.IssueEndpoint,
			Handler: NewGetIssueHandler(h.usecase.NewGetIssueUsecase(), h.guiBase),
		},
		{
			Pattern: "GET " + server.GoroutineEndpoint,
			Handler: NewGetGoroutineHandler(h.usecase.NewGetGoroutineDetailUsecase(), h.guiBase),
		},
		{
			Pattern: "POST " + server.StopEndpoint,
			Handler: NewPostStopHandler(h.usecase.NewWaitAppShutdownUsecase(), h.stop),
		},
	}
}
