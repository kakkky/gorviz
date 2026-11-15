package handler

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/kakkky/gorviz/presentation/view/pages"
)

// AgentCookie は画面で選んでいる agent の名前を持つ cookie
const AgentCookie = "gorviz_agent"

type GetAgentsHandler struct {
	agents  func() []AgentJSON
	current func(r *http.Request) string
}

func NewGetAgentsHandler(agents func() []AgentJSON, current func(r *http.Request) string) *GetAgentsHandler {
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

// SelectAgent は agent を選んで Overview に戻る
func SelectAgent(rw http.ResponseWriter, r *http.Request) {
	http.SetCookie(rw, &http.Cookie{Name: AgentCookie, Value: r.PathValue("name"), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(rw, r, "/", http.StatusSeeOther)
}
