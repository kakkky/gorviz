package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type issueSummaryResponse struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Certainty   string `json:"certainty"`
	Occurrences int    `json:"occurrences"`
	FirstPhase  string `json:"firstPhase"`
	First       string `json:"first"`
	Last        string `json:"last"`
	Value       string `json:"value"`
}

type issuesResponse struct {
	Issues []issueSummaryResponse `json:"issues"`
	More   string                 `json:"more,omitempty"`
	Next   []string               `json:"next"`
	GUI    string                 `json:"gui"`
}

// GetIssuesHandler は Issue の一覧を、次に調べる先の ID と要点だけで返す
type GetIssuesHandler struct {
	list  *usecase.ListIssuesUsecase
	paths paths
}

func NewGetIssuesHandler(list *usecase.ListIssuesUsecase, guiBase string) *GetIssuesHandler {
	return &GetIssuesHandler{list: list, paths: paths{base: guiBase}}
}

func (h *GetIssuesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := domain.IssueKind(strings.ToUpper(q.Get("kind")))
	if kind != "" && !containsKind(kind) {
		middleware.Fail(r, domain.ErrInvalidArgument.With(fmt.Sprintf("kind %q is not an issue kind; use one of %v", q.Get("kind"), domain.IssueKinds)))
		return
	}
	out, err := h.list.Exec(r.Context(), usecase.ListIssuesUsecaseInput{Kind: kind, Text: q.Get("text")})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	resp := issuesResponse{Issues: []issueSummaryResponse{}, GUI: h.paths.base + "/issues"}
	after := q.Get("after")
	skipping := after != ""
	size := 0
	for _, is := range out.Issues {
		if skipping {
			skipping = string(is.ID) != after
			continue
		}
		item := issueSummaryResponse{ID: string(is.ID), Kind: string(is.Kind), Target: is.Target, Certainty: string(is.Certainty), Occurrences: is.Occurrences,
			FirstPhase: is.FirstPhase, First: at(is.FirstAt), Last: at(is.LastAt), Value: is.Value}
		size += len(item.Target) + len(item.Value) + 80
		if size > pageBudget && len(resp.Issues) > 0 {
			resp.More = "gorviz issues --after " + resp.Issues[len(resp.Issues)-1].ID
			break
		}
		resp.Issues = append(resp.Issues, item)
	}
	if len(resp.Issues) > 0 {
		resp.Next = []string{"gorviz issue " + resp.Issues[0].ID, "gorviz docs issues"}
	} else {
		resp.Next = []string{"gorviz diagnose <scenario.yaml>", "gorviz docs scenario"}
	}
	if err := write(rw, r, resp, 0); err != nil {
		middleware.Fail(r, err)
	}
}

func containsKind(k domain.IssueKind) bool {
	for _, v := range domain.IssueKinds {
		if v == k {
			return true
		}
	}
	return false
}
