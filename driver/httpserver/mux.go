package httpserver

import (
	"net/http"

	"github.com/kakkky/gorviz/presentation/handler"
)

func NewMux(h *handler.Handler) http.Handler {
	mux := http.NewServeMux()
	for _, route := range h.Routes() {
		mux.Handle(route.Pattern, route.Handler)
	}
	return mux
}
