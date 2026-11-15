package httpserver

import (
	"net/http"

	"github.com/kakkky/gorviz/assets"
	"github.com/kakkky/gorviz/driver/session"
	"github.com/kakkky/gorviz/presentation/handler"
	"github.com/kakkky/gorviz/usecase"
)

// NewServerMux は server の入口。agent の一覧と JSON の API はここで受け、それ以外の画面は選んでいる agent の Session に渡す。
// agent は ?agent= か cookie で選び、どちらもなければ最初につながった agent を使う
func NewServerMux(registry *session.Registry) http.Handler {
	find := func(r *http.Request) (*session.Session, bool) {
		name := r.URL.Query().Get("agent")
		if name == "" {
			if c, err := r.Cookie(handler.AgentCookie); err == nil {
				name = c.Value
			}
		}
		if s, ok := registry.Get(name); ok {
			return s, true
		}
		return registry.Default()
	}
	agents := func() []handler.AgentJSON {
		var out []handler.AgentJSON
		for _, s := range registry.List() {
			out = append(out, handler.AgentJSON{Name: s.Name, Target: s.Target, Connected: s.Client.Connected()})
		}
		return out
	}
	current := func(r *http.Request) string {
		if s, ok := find(r); ok {
			return s.Name + " — " + s.Target
		}
		return "no agent"
	}

	mux := http.NewServeMux()
	api := handler.NewAPI(func(r *http.Request) (*usecase.UseCase, bool) {
		s, ok := find(r)
		if !ok {
			return nil, false
		}
		return s.UseCase, true
	}, agents)
	for _, route := range api.Routes() {
		mux.Handle(route.Pattern, route.Handler)
	}
	agentsPage := handler.NewGetAgentsHandler(agents, current)
	mux.Handle("GET /agents", agentsPage)
	mux.HandleFunc("GET /agents/{name}/select", handler.SelectAgent)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets.FS())))
	mux.Handle("/", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		s, ok := find(r)
		if !ok {
			agentsPage.ServeHTTP(rw, r)
			return
		}
		s.Handler.ServeHTTP(rw, r)
	}))
	return mux
}
