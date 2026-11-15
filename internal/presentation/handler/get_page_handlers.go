package handler

import (
	"log/slog"
	"net/http"

	"github.com/kakkky/gorviz/internal/presentation/view/pages"
)

const snapshotStreamURL = "/snapshots/stream"

// GetOverviewHandler と GetSchedulerHandler は枠だけを返し、中身は SSE (snapshot stream) で届く
type GetOverviewHandler struct {
	targetURL string
}

func NewGetOverviewHandler(targetURL string) *GetOverviewHandler {
	return &GetOverviewHandler{targetURL: targetURL}
}

func (h *GetOverviewHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	if err := pages.Overview(pages.OverviewViewModel{TargetURL: h.targetURL, StreamURL: snapshotStreamURL}).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render overview", slog.Any("err", err))
	}
}

type GetSchedulerHandler struct {
	targetURL string
}

func NewGetSchedulerHandler(targetURL string) *GetSchedulerHandler {
	return &GetSchedulerHandler{targetURL: targetURL}
}

func (h *GetSchedulerHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	if err := pages.Scheduler(pages.SchedulerViewModel{TargetURL: h.targetURL, StreamURL: snapshotStreamURL}).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render scheduler", slog.Any("err", err))
	}
}
