// Package route は HTTP の経路と、経路ごとの middleware の順序をまとめて組む
package route

import (
	"fmt"
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

// Login はブラウザのログインの handler
type Login struct {
	Get        http.Handler
	Post       http.Handler
	PostLogout http.Handler
}

// GUI はブラウザの画面の handler。経路は domain.GUI が作る URL と揃える
type GUI struct {
	Root            http.Handler
	Assets          http.Handler
	Overview        http.Handler
	OverviewStream  http.Handler
	Issues          http.Handler
	Issue           http.Handler
	Logs            http.Handler
	LogGoroutine    http.Handler
	Scheduler       http.Handler
	SchedulerStream http.Handler
	Goroutine       http.Handler
}

// NewMux は HTTP API (トークン) と、ログインと、GUI (ログイン) を 1 つの経路にまとめる。
// api は serverapi.Route の Name から handler への対応で、serverapi.AllRoutes のすべてが要る。assets はログインの画面も使うのでログインを求めない
func NewMux(mw *middleware.Middleware, api map[string]http.Handler, login Login, gui GUI) (http.Handler, error) {
	mux := http.NewServeMux()
	for _, r := range serverapi.AllRoutes {
		h, ok := api[r.Name]
		if !ok {
			return nil, fmt.Errorf("no handler for the api route %s", r.Name)
		}
		mux.Handle(r.Pattern, mw.RequireToken(h))
	}
	mux.Handle("/api/", mw.RequireToken(http.NotFoundHandler()))
	mux.Handle("GET /login", login.Get)
	mux.Handle("POST /login", login.Post)
	mux.Handle("POST /logout", login.PostLogout)
	mux.Handle("GET /assets/", gui.Assets)
	for pattern, h := range map[string]http.Handler{
		"GET /{$}":                                                            gui.Root,
		"GET /services/{service}/overview":                                    gui.Overview,
		"GET /services/{service}/overview/stream":                             gui.OverviewStream,
		"GET /services/{service}/issues":                                      gui.Issues,
		"GET /services/{service}/issues/{issue}":                              gui.Issue,
		"GET /services/{service}/logs":                                        gui.Logs,
		"GET /services/{service}/logs/{log}/goroutine":                        gui.LogGoroutine,
		"GET /services/{service}/instances/{instance}/scheduler":              gui.Scheduler,
		"GET /services/{service}/instances/{instance}/scheduler/stream":       gui.SchedulerStream,
		"GET /services/{service}/instances/{instance}/goroutines/{goroutine}": gui.Goroutine,
	} {
		mux.Handle(pattern, mw.RequireLogin(h))
	}
	return mw.Handle(mux), nil
}
