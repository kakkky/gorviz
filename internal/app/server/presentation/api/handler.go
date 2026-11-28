package api

import (
	"context"
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// Handler は cli に返す HTTP API の handler を、usecase から組み立てて並べる
type Handler struct {
	usecase   *usecase.UseCase
	guiBase   string
	serverLog string
	stop      context.CancelFunc
}

// NewHandler の stop は、server を止める。アプリの起動に失敗したときと、gorviz stop で呼ぶ
func NewHandler(uc *usecase.UseCase, guiBase, serverLog string, stop context.CancelFunc) *Handler {
	return &Handler{usecase: uc, guiBase: guiBase, serverLog: serverLog, stop: stop}
}

type Route struct {
	Pattern string
	Handler http.Handler
}

func (h *Handler) Routes() []Route {
	return []Route{
		{
			Pattern: "GET /api/status",
			Handler: NewGetStatusHandler(h.usecase.NewGetSessionStatusUsecase(), h.guiBase),
		},
		{
			Pattern: "GET /api/start-result",
			Handler: NewGetStartResultHandler(h.usecase.NewWaitAppStartUsecase(), h.stop, h.guiBase, h.serverLog),
		},
		{
			Pattern: "POST /api/diagnose",
			Handler: NewPostDiagnoseHandler(h.usecase.NewDiagnoseAppUsecase(), h.guiBase),
		},
		{
			Pattern: "GET /api/issues",
			Handler: NewGetIssuesHandler(h.usecase.NewListIssuesUsecase(), h.guiBase),
		},
		{
			Pattern: "GET /api/issues/{id}",
			Handler: NewGetIssueHandler(h.usecase.NewGetIssueUsecase(), h.guiBase),
		},
		{
			Pattern: "GET /api/goroutines/{id}",
			Handler: NewGetGoroutineHandler(h.usecase.NewGetGoroutineDetailUsecase(), h.guiBase),
		},
		{
			Pattern: "POST /api/stop",
			Handler: NewPostStopHandler(h.usecase.NewWaitAppShutdownUsecase(), h.stop),
		},
	}
}
