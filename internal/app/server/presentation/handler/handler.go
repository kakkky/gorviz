package handler

import (
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// Turbo は HTML 以外の応答をフレームに描画しない。断片 (<turbo-frame> から始まる) は Go の推定だと text/plain になるので明示する
const htmlContentType = "text/html; charset=utf-8"

type Handler struct {
	cfg     *config.Config
	usecase *usecase.UseCase
}

func NewHandler(cfg *config.Config, usecase *usecase.UseCase) *Handler {
	return &Handler{
		cfg:     cfg,
		usecase: usecase,
	}
}

type Route struct {
	Pattern string
	Handler http.Handler
}

func (h *Handler) Routes() []Route {
	return []Route{
		{
			Pattern: "GET /{$}",
			Handler: NewGetOverviewHandler(h.cfg.TargetURL),
		},
		{
			Pattern: "GET /issues",
			Handler: NewGetIssuesHandler(h.cfg.TargetURL),
		},
		{
			Pattern: "GET /issues/stream",
			Handler: NewGetIssuesStreamHandler(h.usecase.NewListIssueHistoryUsecase(), h.cfg.Refresh),
		},
		{
			Pattern: "GET /issues/detail",
			Handler: NewGetIssueHandler(h.cfg.TargetURL),
		},
		{
			Pattern: "GET /issues/detail/stream",
			Handler: NewGetIssueStreamHandler(h.usecase.NewGetIssueHistoryUsecase(), h.cfg.Refresh),
		},
		{
			Pattern: "GET /logs",
			Handler: NewGetLogsHandler(h.usecase.NewListLogsUsecase(), h.cfg.TargetURL),
		},
		{
			Pattern: "GET /logs/stream",
			Handler: NewGetLogsStreamHandler(h.usecase.NewListLogsUsecase(), h.cfg.Refresh),
		},
		{
			Pattern: "GET /logs/{id}/open",
			Handler: NewGetLogOpenHandler(h.usecase.NewLocateLogUsecase(), h.cfg.TraceWindow),
		},
		{
			Pattern: "GET /scheduler",
			Handler: NewGetSchedulerHandler(h.usecase.NewGetSchedulerReplayUsecase(), h.cfg.TargetURL, h.cfg.TraceWindow),
		},
		{
			Pattern: "GET /scheduler/stream",
			Handler: NewGetSchedulerStreamHandler(h.usecase.NewGetSchedulerReplayUsecase(), h.cfg.Refresh, h.cfg.TraceWindow),
		},
		{
			Pattern: "GET /goroutines/{id}",
			Handler: NewGetGoroutineHandler(h.usecase.NewGetGoroutineUsecase(), h.usecase.NewGetSchedulerReplayUsecase(), h.usecase.NewListIssuesUsecase(), h.cfg.TargetURL, h.cfg.TraceWindow),
		},
		{
			Pattern: "GET /overview/stream",
			Handler: NewGetOverviewStreamHandler(h.usecase.NewGetRuntimeSnapshotUsecase(), h.usecase.NewListIssueHistoryUsecase(), h.usecase.NewGetMetricsUsecase(), h.cfg.Refresh, domain.IssuePolicy{CPUWaitRatio: h.cfg.CPUWaitRatio, GCPerSecond: h.cfg.GCPerSecond}),
		},
	}
}
