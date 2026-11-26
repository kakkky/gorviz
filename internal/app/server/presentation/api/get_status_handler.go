package api

import (
	"net/http"
	"strings"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type appResponse struct {
	Package          string   `json:"package"`
	Args             []string `json:"args,omitempty"`
	State            string   `json:"state"`
	PID              int      `json:"pid,omitempty"`
	GoVersion        string   `json:"goVersion,omitempty"`
	Procs            int      `json:"procs,omitempty"`
	ExitCode         *int     `json:"exitCode,omitempty"`
	TraceTruncated   bool     `json:"traceTruncated,omitempty"`
	ShutdownTimedOut bool     `json:"shutdownTimedOut,omitempty"`
	Error            string   `json:"error,omitempty"`
}

func appOf(a domain.AppRun) appResponse {
	r := appResponse{Package: a.Package, Args: a.Args, State: string(a.State), PID: a.PID, GoVersion: a.GoVersion, Procs: a.Procs, TraceTruncated: a.TraceTruncated, ShutdownTimedOut: a.ShutdownTimedOut}
	if a.Exited {
		code := a.ExitCode
		r.ExitCode = &code
	}
	r.Error = strings.TrimSpace(a.BuildError + a.StartError)
	return r
}

type eventResponse struct {
	At    string `json:"at"`
	Event string `json:"event"`
	Phase string `json:"phase,omitempty"`
	Note  string `json:"note,omitempty"`
}

type rangeResponse struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type statusResponse struct {
	App            appResponse     `json:"app"`
	AgentConnected bool            `json:"agentConnected"`
	Elapsed        string          `json:"elapsed"`
	Diagnosing     bool            `json:"diagnosing,omitempty"`
	Diagnosed      []rangeResponse `json:"diagnosed"`
	Issues         int             `json:"issues"`
	Timeline       []eventResponse `json:"timeline"`
	GUI            string          `json:"gui"`
	Next           []string        `json:"next"`
}

// GetStatusHandler はアプリが動いているか (終わっていれば終了コード)・GUI の URL・これまでに診断した時間帯を返す
type GetStatusHandler struct {
	status  *usecase.GetSessionStatusUsecase
	guiBase string
}

func NewGetStatusHandler(status *usecase.GetSessionStatusUsecase, guiBase string) *GetStatusHandler {
	return &GetStatusHandler{status: status, guiBase: guiBase}
}

func (h *GetStatusHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	out, err := h.status.Exec(r.Context(), usecase.GetSessionStatusUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	resp := statusResponse{App: appOf(out.Session.App), AgentConnected: out.AgentConnected, Elapsed: at(out.Elapsed), Diagnosing: out.Session.Diagnosing,
		Diagnosed: []rangeResponse{}, Issues: out.Issues, GUI: h.guiBase}
	for _, d := range out.Session.Diagnoses {
		resp.Diagnosed = append(resp.Diagnosed, rangeResponse{From: at(d.From), To: at(d.To)})
	}
	for _, e := range out.Timeline {
		resp.Timeline = append(resp.Timeline, eventResponse{At: at(e.At), Event: string(e.Kind), Phase: e.Phase, Note: e.Note})
	}
	switch {
	case out.Issues > 0:
		resp.Next = []string{"gorviz issues"}
	case out.Session.App.State == domain.AppRunning:
		resp.Next = []string{"gorviz diagnose <scenario.yaml>", "gorviz docs scenario"}
	default:
		resp.Next = []string{"gorviz stop"}
	}
	if err := write(rw, r, resp, 0); err != nil {
		middleware.Fail(r, err)
	}
}
