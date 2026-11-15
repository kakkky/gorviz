package handler

import (
	"net/http"

	"github.com/kakkky/gorviz/assets"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/usecase"
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
			Handler: NewGetDashboardHandler(h.cfg.TargetURL),
		},
		{
			Pattern: "GET /snapshots/stream",
			Handler: NewGetSnapshotStreamHandler(h.usecase.NewGetRuntimeSnapshotUsecase(), h.usecase.NewGetSchedTraceUsecase(), h.cfg.Refresh, h.cfg.TimelineBuckets),
		},
		{
			Pattern: "GET /explore",
			Handler: NewGetExploreHandler(h.usecase.NewListLogsUsecase(), h.cfg.TargetURL),
		},
		{
			Pattern: "GET /explore/logs/{id}/replay",
			Handler: NewGetLogReplayHandler(h.usecase.NewGetLogReplayUsecase()),
		},
		{
			Pattern: "GET /assets/",
			Handler: http.StripPrefix("/assets/", http.FileServerFS(assets.FS())),
		},
	}
}
