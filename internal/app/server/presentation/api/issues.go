package api

import (
	"fmt"
	"net/http"
	"strconv"
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

type occurrenceResponse struct {
	N         int    `json:"n"`
	From      string `json:"from"`
	To        string `json:"to"`
	Phase     string `json:"phase"`
	Certainty string `json:"certainty"`
	Value     string `json:"value"`
}

type leakStackResponse struct {
	CreatedBy  string  `json:"createdBy"`
	BlockedAt  string  `json:"blockedAt"`
	Reason     string  `json:"reason,omitempty"`
	Leaked     string  `json:"leaked"`
	Certainty  string  `json:"certainty"`
	Goroutines []int64 `json:"goroutines,omitempty"`
}

type lockHolderResponse struct {
	Function  string `json:"function"`
	Location  string `json:"location"`
	Goroutine int64  `json:"goroutine"`
	Next      string `json:"next"`
}

type lockSiteResponse struct {
	Function  string               `json:"function"`
	Location  string               `json:"location"`
	WaitShare string               `json:"waitShare"`
	Waited    string               `json:"waited"`
	Count     int                  `json:"count"`
	Mean      string               `json:"mean"`
	Max       string               `json:"max"`
	HeldBy    []lockHolderResponse `json:"heldBy,omitempty"`
}

type issueDetailResponse struct {
	ID          string               `json:"id"`
	Kind        string               `json:"kind"`
	Target      string               `json:"target"`
	Certainty   string               `json:"certainty"`
	FirstPhase  string               `json:"firstPhase"`
	NextAction  string               `json:"nextAction"`
	Occurrences []occurrenceResponse `json:"occurrences"`
	Omitted     int                  `json:"omittedOccurrences,omitempty"`
	Showing     int                  `json:"showingOccurrence"`
	Range       string               `json:"range"`
	Leaks       []leakStackResponse  `json:"leaks,omitempty"`
	LockSites   []lockSiteResponse   `json:"lockSites,omitempty"`
	Goroutines  []goroutineResponse  `json:"goroutines"`
	GUI         string               `json:"gui"`
	Scheduler   string               `json:"scheduler"`
	Next        []string             `json:"next"`
}

const maxOccurrences = 20

// GetIssueHandler は Issue の発生の時間軸と、影響の大きかった goroutine の詳細まで返す。Issue を開くだけで原因まで読めるようにする
type GetIssueHandler struct {
	get   *usecase.GetIssueUsecase
	paths paths
}

func NewGetIssueHandler(get *usecase.GetIssueUsecase, guiBase string) *GetIssueHandler {
	return &GetIssueHandler{get: get, paths: paths{base: guiBase}}
}

func (h *GetIssueHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	n := 0
	if v := r.URL.Query().Get("occurrence"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			middleware.Fail(r, domain.ErrInvalidArgument.With(fmt.Sprintf("occurrence %q must be a number from 1", v)))
			return
		}
	}
	out, err := h.get.Exec(r.Context(), usecase.GetIssueUsecaseInput{ID: domain.IssueID(strings.ToUpper(r.PathValue("id"))), Occurrence: n})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	is := out.Issue
	resp := issueDetailResponse{ID: string(is.ID), Kind: string(is.Kind), Target: is.TargetLabel(), Certainty: string(out.Certainty), FirstPhase: is.FirstPhase(), NextAction: out.NextAction,
		Showing: out.Occurrence, Range: at(out.Impact.From) + " to " + at(out.Impact.To), Goroutines: []goroutineResponse{},
		GUI: h.paths.issue(is.ID, out.Occurrence), Scheduler: h.paths.scheduler(out.Impact.From, out.Impact.To)}
	start := max(0, len(is.Occurrences)-maxOccurrences)
	resp.Omitted = start
	for i, o := range is.Occurrences[start:] {
		resp.Occurrences = append(resp.Occurrences, occurrenceResponse{N: start + i + 1, From: at(o.From), To: at(o.To), Phase: o.Phase, Certainty: string(o.Certainty), Value: o.Value})
	}
	for _, l := range out.Impact.Leaks {
		ls := leakStackResponse{CreatedBy: frame(l.Creator), BlockedAt: frame(l.Blocked), Reason: l.Reason, Certainty: string(l.Certainty),
			Leaked: fmt.Sprintf("%d of the %d goroutines waiting at the same place leaked", l.Count, l.Of)}
		for i, g := range l.Goroutines {
			if i == 10 {
				break
			}
			ls.Goroutines = append(ls.Goroutines, int64(g))
		}
		resp.Leaks = append(resp.Leaks, ls)
	}
	for _, s := range out.Impact.LockSites {
		site := lockSiteResponse{Function: s.Function, Location: s.Location, WaitShare: fmt.Sprintf("%.0f%%", s.Share*100), Waited: dur(s.Wait), Count: s.Count, Mean: dur(s.Mean), Max: dur(s.Max)}
		for _, hd := range s.Holders {
			site.HeldBy = append(site.HeldBy, lockHolderResponse{Function: hd.Function, Location: hd.Location, Goroutine: int64(hd.G), Next: goroutineCommand(hd.G, hd.WaitFrom, hd.WaitTo)})
		}
		resp.LockSites = append(resp.LockSites, site)
	}
	for _, g := range out.Impact.Goroutines {
		gr := goroutineOf(h.paths, g.Detail, false)
		if g.Note != "" {
			gr.Note = g.Note
		}
		if g.Duration > 0 {
			gr.Value = valueLabel(is.Kind) + " " + dur(g.Duration)
		}
		resp.Goroutines = append(resp.Goroutines, gr)
		for _, w := range g.Detail.Wakers {
			resp.Next = append(resp.Next, goroutineCommand(w.G, w.WaitFrom, w.WaitTo))
		}
	}
	for _, s := range resp.LockSites {
		for _, hd := range s.HeldBy {
			resp.Next = append(resp.Next, hd.Next)
		}
	}
	if len(resp.Next) > 5 {
		resp.Next = resp.Next[:5]
	}
	resp.Next = append(resp.Next, "gorviz docs issues")
	if err := write(rw, r, resp, 0); err != nil {
		middleware.Fail(r, err)
	}
}

func valueLabel(k domain.IssueKind) string {
	switch k {
	case domain.IssueCPUContention:
		return "RUNNABLE for"
	case domain.IssueLockContention:
		return "waited on locks for"
	case domain.IssueGCPressure:
		return "mark assist for"
	case domain.IssueLongSTW:
		return "stopped the world for"
	case domain.IssueThreadExplosion:
		return "in syscalls for"
	}
	return ""
}
