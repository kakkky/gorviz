package httpserver

import (
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/assets"
	"github.com/kakkky/gorviz/internal/app/server/presentation/handler"
)

// NewMux は routes を登録し、画面が読む静的ファイル (/assets/) を足す
func NewMux(routes []handler.Route) http.Handler {
	mux := http.NewServeMux()
	for _, route := range routes {
		mux.Handle(route.Pattern, route.Handler)
	}
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets.FS())))
	return mux
}

// NewRouteMux は routes だけを登録する。agent ごとの画面に使う
func NewRouteMux(routes []handler.Route) http.Handler {
	mux := http.NewServeMux()
	for _, route := range routes {
		mux.Handle(route.Pattern, route.Handler)
	}
	return mux
}
