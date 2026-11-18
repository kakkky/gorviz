package handler

import (
	"cmp"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/kakkky/gorviz/internal/app/api/serverapi"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// Session は agent 1 つ分の画面と API の向け先
type Session struct {
	Name string
	// Service は対象のアプリの名前。空なら agent の名前で 1 つだけのまとまりにする
	Service   string
	Target    string
	Connected func() bool
	UseCase   *usecase.UseCase
	// Handler はその agent の画面 (Routes を登録したもの)
	Handler http.Handler
}

// Sessions は server がつながった agent の Session を名前で引く口
type Sessions interface {
	Get(name string) (Session, bool)
	// Default は名前の指定がないときに使う、最初につながった agent
	Default() (Session, bool)
	List() []Session
}

// AgentRouter は server の入口。agent の一覧と JSON の API はここで受け、それ以外の画面は選んでいる agent の Session に渡す。
// agent は ?agent= か cookie で選び、どちらもなければ最初につながった agent を使う
type AgentRouter struct {
	sessions Sessions
}

func NewAgentRouter(sessions Sessions) *AgentRouter {
	return &AgentRouter{sessions: sessions}
}

func (a *AgentRouter) Routes() []Route {
	api := NewAPI(func(r *http.Request) (*usecase.UseCase, bool) {
		s, ok := a.find(r)
		return s.UseCase, ok
	}, a.agents)
	agentsPage := NewGetAgentsHandler(a.agents, a.current)
	routes := append(api.Routes(),
		Route{Pattern: "GET /agents", Handler: agentsPage},
		Route{Pattern: "GET /agents/menu", Handler: http.HandlerFunc(a.menu)},
		Route{Pattern: "GET /agents/{name}/select", Handler: http.HandlerFunc(SelectAgent)},
		Route{Pattern: "/", Handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			s, ok := a.find(r)
			if !ok {
				agentsPage.ServeHTTP(rw, r)
				return
			}
			s.Handler.ServeHTTP(rw, r)
		})},
	)
	return routes
}

func (a *AgentRouter) find(r *http.Request) (Session, bool) {
	name := r.URL.Query().Get("agent")
	if name == "" {
		if c, err := r.Cookie(AgentCookie); err == nil {
			name = c.Value
		}
	}
	if s, ok := a.sessions.Get(name); ok {
		return s, true
	}
	return a.sessions.Default()
}

func (a *AgentRouter) agents() []serverapi.AgentJSON {
	var out []serverapi.AgentJSON
	for _, s := range a.sessions.List() {
		out = append(out, serverapi.AgentJSON{Name: s.Name, Service: s.Service, Target: s.Target, Connected: s.Connected()})
	}
	return out
}

func (a *AgentRouter) current(r *http.Request) string {
	if s, ok := a.find(r); ok {
		return s.Name + " — " + s.Target
	}
	return "no agent"
}

// menu は見出しの、見ている対象を切り替える部品。service ごとにインスタンスを並べ、選ぶと今の画面のまま切り替える (戻る先は Referer)
func (a *AgentRouter) menu(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	current, ok := a.find(r)
	vm := components.AgentMenuViewModel{Empty: !ok}
	back := ""
	if ref, err := url.Parse(r.Referer()); err == nil {
		back = ref.RequestURI()
	}
	groups := map[string]int{}
	for _, s := range a.sessions.List() {
		item := components.AgentMenuItemViewModel{
			Service: s.Service, Name: s.Name, Target: s.Target, Connected: s.Connected(), Current: ok && s.Name == current.Name,
			SelectURL: "/agents/" + url.PathEscape(s.Name) + "/select?" + url.Values{"back": {back}}.Encode(),
		}
		if item.Current {
			vm.Current = item
		}
		service := cmp.Or(s.Service, s.Name)
		i, seen := groups[service]
		if !seen {
			i = len(vm.Groups)
			groups[service] = i
			vm.Groups = append(vm.Groups, components.AgentMenuGroupViewModel{Service: service})
		}
		vm.Groups[i].Agents = append(vm.Groups[i].Agents, item)
	}
	if err := components.AgentMenu(vm).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render agent menu", slog.Any("err", err))
	}
}
