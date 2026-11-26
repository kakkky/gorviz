package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type stopResponse struct {
	App       appResponse          `json:"app"`
	Finished  bool                 `json:"shutdownFinishedInTime"`
	Remaining []leakStackResponse  `json:"remainingGoroutines,omitempty"`
	Issues    []diagnosedIssueBody `json:"issues,omitempty"`
}

// PostStopHandler は server を止める口を引き、アプリの shutdown を見届けてから、その結果を返す
type PostStopHandler struct {
	wait *usecase.WaitAppShutdownUsecase
	stop context.CancelFunc
}

func NewPostStopHandler(wait *usecase.WaitAppShutdownUsecase, stop context.CancelFunc) *PostStopHandler {
	return &PostStopHandler{wait: wait, stop: stop}
}

func (h *PostStopHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	h.stop()
	out, err := h.wait.Exec(r.Context(), usecase.WaitAppShutdownUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	resp := stopResponse{App: appOf(out.App), Finished: !out.App.ShutdownTimedOut}
	for _, l := range out.Remaining {
		resp.Remaining = append(resp.Remaining, leakStackResponse{CreatedBy: frame(l.Creator), BlockedAt: frame(l.Blocked), Reason: l.Reason, Certainty: string(l.Certainty),
			Leaked: fmt.Sprintf("%d of the %d goroutines waiting at the same place leaked", l.Count, l.Of)})
	}
	for _, is := range out.Issues {
		resp.Issues = append(resp.Issues, diagnosedIssueOf(is))
	}
	if err := write(rw, r, resp, 0); err != nil {
		middleware.Fail(r, err)
	}
}

type diagnosedIssueBody struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Certainty   string `json:"certainty"`
	FirstPhase  string `json:"firstPhase"`
	Occurrences int    `json:"occurrences"`
}

func diagnosedIssueOf(is domain.DiagnosedIssue) diagnosedIssueBody {
	return diagnosedIssueBody{ID: string(is.ID), Kind: string(is.Kind), Target: is.Target, Certainty: string(is.Certainty), FirstPhase: is.FirstPhase, Occurrences: is.Occurrences}
}
