package handler

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/kakkky/gorviz/internal/app/protocol/serverapi"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
)

// AgentCookie は画面で選んでいる agent の名前を持つ cookie
const AgentCookie = "gorviz_agent"

type GetAgentsHandler struct {
	agents  func() []serverapi.AgentJSON
	current func(r *http.Request) string
}

func NewGetAgentsHandler(agents func() []serverapi.AgentJSON, current func(r *http.Request) string) *GetAgentsHandler {
	return &GetAgentsHandler{agents: agents, current: current}
}

func (h *GetAgentsHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	vm := pages.AgentsViewModel{Current: h.current(r)}
	for _, a := range h.agents() {
		vm.Agents = append(vm.Agents, pages.AgentViewModel{Name: a.Name, Target: a.Target, Connected: a.Connected, SelectURL: "/agents/" + url.PathEscape(a.Name) + "/select"})
	}
	if err := pages.Agents(vm).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render agents", slog.Any("err", err))
	}
}

// SelectAgent は agent を選び、?back= の画面 (切り替えたときに見ていた画面) に戻る。
// Goroutine の画面は agent ごとの G なので、別の agent では Overview に戻る。back がこの server の中の道でなければ Overview
func SelectAgent(rw http.ResponseWriter, r *http.Request) {
	http.SetCookie(rw, &http.Cookie{Name: AgentCookie, Value: r.PathValue("name"), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(rw, r, backAfterSelect(r.URL.Query().Get("back")), http.StatusSeeOther)
}

func backAfterSelect(back string) string {
	u, err := url.Parse(back)
	if err != nil || !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") || u.Host != "" ||
		strings.HasPrefix(u.Path, "/goroutines/") || strings.HasPrefix(u.Path, "/agents") {
		return "/"
	}
	q := u.Query()
	q.Del("agent")
	u.RawQuery = q.Encode()
	return u.RequestURI()
}
