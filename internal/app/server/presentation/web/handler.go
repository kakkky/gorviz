package web

import (
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/assets"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// Handler は GUI の画面の handler を、usecase から組み立てて並べる
type Handler struct {
	usecase *usecase.UseCase
}

func NewHandler(uc *usecase.UseCase) *Handler {
	return &Handler{usecase: uc}
}

type Route struct {
	Pattern string
	Handler http.Handler
}

func (h *Handler) Routes() []Route {
	return []Route{
		{
			Pattern: "GET /{$}",
			Handler: NewGetRootHandler(),
		},
		{
			Pattern: "GET /overview",
			Handler: NewGetOverviewHandler(h.usecase.NewGetSessionStatusUsecase(), h.usecase.NewGetOverviewUsecase()),
		},
		{
			Pattern: "GET /overview/stream",
			Handler: NewGetOverviewStreamHandler(h.usecase.NewGetSessionStatusUsecase(), h.usecase.NewGetOverviewUsecase()),
		},
		{
			Pattern: "GET /issues",
			Handler: NewGetIssuesHandler(h.usecase.NewGetSessionStatusUsecase(), h.usecase.NewListIssuesUsecase()),
		},
		{
			Pattern: "GET /issues/{id}",
			Handler: NewGetIssueHandler(h.usecase.NewGetSessionStatusUsecase(), h.usecase.NewGetIssueUsecase()),
		},
		{
			Pattern: "GET /scheduler",
			Handler: NewGetSchedulerHandler(h.usecase.NewGetSessionStatusUsecase(), h.usecase.NewGetSchedulerWindowUsecase()),
		},
		{
			Pattern: "GET /scheduler/stream",
			Handler: NewGetSchedulerStreamHandler(h.usecase.NewGetSchedulerWindowUsecase()),
		},
		{
			Pattern: "GET /goroutines/{id}",
			Handler: NewGetGoroutineHandler(h.usecase.NewGetSessionStatusUsecase(), h.usecase.NewGetGoroutineDetailUsecase(), h.usecase.NewGetSchedulerWindowUsecase()),
		},
		{
			Pattern: "GET " + assets.URLPrefix,
			Handler: NewGetAssetsHandler(),
		},
	}
}
