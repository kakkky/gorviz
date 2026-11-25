package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// GetIssuesHandler は Issue の一覧を描く
type GetIssuesHandler struct {
	status *usecase.GetSessionStatusUsecase
	list   *usecase.ListIssuesUsecase
}

func NewGetIssuesHandler(status *usecase.GetSessionStatusUsecase, list *usecase.ListIssuesUsecase) *GetIssuesHandler {
	return &GetIssuesHandler{status: status, list: list}
}

func (h *GetIssuesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	st, err := h.status.Exec(r.Context(), usecase.GetSessionStatusUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	q := r.URL.Query()
	kind := domain.IssueKind(strings.ToUpper(q.Get("kind")))
	out, err := h.list.Exec(r.Context(), usecase.ListIssuesUsecaseInput{Kind: kind, Text: q.Get("text")})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	vm := pages.IssuesViewModel{Base: baseOf("Issues", tabIssues, st, focus{}), Kind: string(kind), Text: q.Get("text")}
	for _, k := range domain.IssueKinds {
		vm.Kinds = append(vm.Kinds, string(k))
	}
	for _, is := range out.Issues {
		vm.Issues = append(vm.Issues, pages.IssueRowViewModel{ID: string(is.ID), URL: issueURL(is.ID, 0), Kind: string(is.Kind), Target: is.Target, Certainty: string(is.Certainty),
			Occurrences: is.Occurrences, FirstPhase: is.FirstPhase, First: at(is.FirstAt), Last: at(is.LastAt), Value: is.Value})
	}
	render(rw, r, pages.Issues(vm))
}

// GetIssueHandler は 1 つの Issue と、選んだ発生で影響の大きかった goroutine を描く
type GetIssueHandler struct {
	status *usecase.GetSessionStatusUsecase
	get    *usecase.GetIssueUsecase
}

func NewGetIssueHandler(status *usecase.GetSessionStatusUsecase, get *usecase.GetIssueUsecase) *GetIssueHandler {
	return &GetIssueHandler{status: status, get: get}
}

func (h *GetIssueHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	n := 0
	if v := r.URL.Query().Get("occurrence"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 1 {
			middleware.Fail(r, domain.ErrInvalidArgument.With("occurrence must be a number from 1"))
			return
		}
	}
	st, err := h.status.Exec(r.Context(), usecase.GetSessionStatusUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	out, err := h.get.Exec(r.Context(), usecase.GetIssueUsecaseInput{ID: domain.IssueID(strings.ToUpper(r.PathValue("id"))), Occurrence: n})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	render(rw, r, pages.Issue(issueViewModel(st, out)))
}

func issueViewModel(st usecase.GetSessionStatusUsecaseOutput, out usecase.GetIssueUsecaseOutput) pages.IssueViewModel {
	is, imp := out.Issue, out.Impact
	vm := pages.IssueViewModel{Base: baseOf(string(is.ID)+" "+string(is.Kind), tabIssues, st, focus{from: imp.From, to: imp.To, has: true}),
		ID: string(is.ID), Kind: string(is.Kind), Target: is.TargetLabel(), Certainty: string(out.Certainty), FirstPhase: is.FirstPhase(), NextAction: out.NextAction,
		Showing: out.Occurrence, Range: span(imp.From, imp.To), SchedulerURL: schedulerURL(imp.From, imp.To)}
	for i, o := range is.Occurrences {
		vm.Occurrences = append(vm.Occurrences, pages.OccurrenceRowViewModel{N: i + 1, URL: issueURL(is.ID, i+1), Current: i+1 == out.Occurrence, Range: span(o.From, o.To), Phase: o.Phase, Certainty: string(o.Certainty), Value: o.Value})
	}
	for _, l := range imp.Leaks {
		row := pages.LeakRowViewModel{CreatedBy: frameText(l.Creator), BlockedAt: frameText(l.Blocked), Reason: l.Reason, Certainty: string(l.Certainty),
			Leaked: fmt.Sprintf("%d of the %d goroutines waiting at the same place leaked", l.Count, l.Of)}
		for i, g := range l.Goroutines {
			if i == 20 {
				break
			}
			row.Goroutines = append(row.Goroutines, pages.LinkRowViewModel{Label: "G" + strconv.FormatInt(int64(g), 10), URL: goroutineURL(g, imp.From, imp.To)})
		}
		vm.Leaks = append(vm.Leaks, row)
	}
	for _, s := range imp.LockSites {
		row := pages.LockSiteRowViewModel{Function: s.Function, Location: s.Location, Share: fmt.Sprintf("%.0f%%", s.Share*100), Count: s.Count, Mean: domain.FormatDuration(s.Mean), Max: domain.FormatDuration(s.Max)}
		for _, hd := range s.Holders {
			row.HeldBy = append(row.HeldBy, pages.LinkRowViewModel{Label: fmt.Sprintf("G%d %s %s", hd.G, hd.Function, hd.Location), URL: goroutineURL(hd.G, hd.WaitFrom, hd.WaitTo)})
		}
		vm.LockSites = append(vm.LockSites, row)
	}
	for _, g := range imp.Goroutines {
		card := cardOf(g.Detail)
		if g.Note != "" {
			card.Note = g.Note
		}
		if g.Duration > 0 {
			card.Value = domain.FormatDuration(g.Duration)
		}
		vm.Goroutines = append(vm.Goroutines, card)
	}
	return vm
}

func frameText(f domain.Frame) string {
	if loc := f.Location(); loc != "" {
		return f.Function + " " + loc
	}
	return f.Function
}

func cardOf(d domain.GoroutineDetail) components.GoroutineCardViewModel {
	vm := components.GoroutineCardViewModel{ID: strconv.FormatInt(int64(d.ID), 10), URL: goroutineURL(d.ID, d.From, d.To), Agent: d.Agent, Entry: d.Entry, CreatedBy: frameText(d.Creator),
		Range: span(d.From, d.To), MainCause: string(d.MainCause), Certainty: string(d.Certainty), Truncated: d.TraceTruncated}
	if d.Agent {
		vm.Note = "a goroutine of the gorviz agent; shown, but never counted in issues"
	}
	if d.Creator.Function == "" {
		vm.CreatedBy = ""
	}
	var total int64
	for _, b := range d.Breakdown {
		total += int64(b.Duration)
	}
	for _, b := range d.Breakdown {
		vm.Breakdown = append(vm.Breakdown, components.CauseShareViewModel{Cause: string(b.Cause), Share: fmt.Sprintf("%.2f%%", float64(b.Duration)*100/float64(max(total, 1))), Duration: domain.FormatDuration(b.Duration)})
	}
	if d.StoppedAt.Function != "" {
		vm.StoppedAt, vm.StoppedIn, vm.StoppedFor, vm.Reason = frameText(d.StoppedAt), d.StoppedIn, domain.FormatDuration(d.StoppedFor), d.StoppedReason
	}
	for _, w := range d.Wakers {
		vm.WokenBy = append(vm.WokenBy, components.WakerViewModel{G: strconv.FormatInt(int64(w.G), 10), URL: goroutineURL(w.G, w.WaitFrom, w.WaitTo), Function: w.Function, Location: w.Location, Waited: domain.FormatDuration(w.Total), Times: w.Count})
	}
	if d.Leak != nil {
		vm.Leak, vm.LeakCert = fmt.Sprintf("%d of the %d goroutines waiting at the same place leaked", d.Leak.Count, d.Leak.Of), string(d.Leak.Certainty)
	}
	if d.Ended {
		vm.Ended = at(d.EndedAt)
	}
	for _, f := range d.Stack {
		vm.Stack = append(vm.Stack, frameText(f))
	}
	return vm
}
