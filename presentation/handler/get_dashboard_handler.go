package handler

import (
	"log/slog"
	"net/http"

	"github.com/kakkky/gorviz/presentation/view/pages"
)

type GetDashboardHandler struct {
	targetURL string
}

func NewGetDashboardHandler(targetURL string) *GetDashboardHandler {
	return &GetDashboardHandler{targetURL: targetURL}
}

func (h *GetDashboardHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	if err := pages.Dashboard(pages.DashboardViewModel{
		TargetURL: h.targetURL,
		StreamURL: "/snapshots/stream",
	}).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render dashboard", slog.Any("err", err))
	}
}
