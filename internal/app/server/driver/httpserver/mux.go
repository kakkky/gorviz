package httpserver

import (
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/presentation/api"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web"
)

func NewAPIMux(h *api.Handler, mw *middleware.Middleware) http.Handler {
	mux := http.NewServeMux()
	for _, route := range h.Routes() {
		mux.Handle(route.Pattern, route.Handler)
	}
	return mw.Wrap(mux)
}

func NewGUIMux(h *web.Handler, mw *middleware.Middleware) http.Handler {
	mux := http.NewServeMux()
	for _, route := range h.Routes() {
		mux.Handle(route.Pattern, route.Handler)
	}
	return mw.Wrap(mux)
}
