package web

import (
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// GetIssueHandler は 1 つの Issue の発生の推移と、発生ごとに影響の大きかった goroutine を描く
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
	tr, err := parseRange(r.URL.Query(), st.Elapsed, fromSession)
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	id := domain.IssueID(strings.ToUpper(r.PathValue("id")))
	out, err := h.get.Exec(r.Context(), usecase.GetIssueUsecaseInput{ID: id, Occurrence: n, AllImpacts: true})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	vm := issueViewModel(st, out, tr)
	vm.Picker = picker("/issues/"+string(id), nil, tr, phasesOf(st.Timeline, st.Elapsed))
	render(rw, r, pages.Issue(vm))
}

func issueViewModel(st usecase.GetSessionStatusUsecaseOutput, out usecase.GetIssueUsecaseOutput, tr timeRange) pages.IssueViewModel {
	is := out.Issue
	first, last := is.Occurrences[0], is.Occurrences[len(is.Occurrences)-1]
	vm := pages.IssueViewModel{Base: baseOf(string(is.ID)+" "+string(is.Kind), tabIssues, st), Kind: string(is.Kind), Subject: is.TargetLabel(), Certainty: string(out.Certainty),
		CertaintyNote: certaintyNote(out.Certainty), Ongoing: ongoing(st, last.To), FirstSeen: at(first.From), LastSeen: at(last.To), FirstPhase: orDash(is.FirstPhase()), NextAction: out.NextAction, IssuesURL: "/issues"}
	var starts []time.Duration
	for i := len(is.Occurrences); i >= 1; i-- {
		o := is.Occurrences[i-1]
		starts = append(starts, o.From)
		if o.To < tr.From || o.From > tr.To {
			continue
		}
		vm.Total++
		ovm := pages.OccurrenceViewModel{Phase: orDash(o.Phase), Period: span(o.From, o.To), Ongoing: ongoing(st, o.To), Judge: judgeOf(o.Certainty), Certainty: string(o.Certainty),
			CertaintyNote: certaintyNote(o.Certainty), Reason: o.Value, Open: i == out.Occurrence}
		if imp, ok := out.Impacts[i]; ok {
			occurrenceDetail(&ovm, is.Kind, imp)
		}
		vm.Occurrences = append(vm.Occurrences, ovm)
	}
	slices.Reverse(starts)
	vm.Counts = occurrenceChart(starts, tr)
	return vm
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func judgeOf(c domain.Certainty) string {
	if c == domain.CertaintyFact {
		return "runtime"
	}
	return "threshold"
}

func occurrenceDetail(vm *pages.OccurrenceViewModel, kind domain.IssueKind, imp domain.Impact) {
	vm.SchedulerURL = schedulerURL(imp.From, imp.To)
	for _, l := range imp.Leaks {
		site := pages.LeakSiteViewModel{CreatedBy: l.Creator.Function, CreatedByLocation: l.Creator.Location(), Function: l.Blocked.Function, Location: l.Blocked.Location(), Reason: l.Reason,
			Leak: fmt.Sprintf("%d of the %d goroutines waiting at the same place leaked", l.Count, l.Of), Certainty: string(l.Certainty), CertaintyNote: certaintyNote(l.Certainty)}
		var ids []string
		for i, g := range l.Goroutines {
			if i == 8 {
				ids = append(ids, "…")
				break
			}
			ids = append(ids, gid(g))
		}
		site.Goroutines = strings.Join(ids, " ")
		if len(l.Goroutines) > 0 {
			site.URL = goroutineURL(l.Goroutines[0], imp.From, imp.To)
		}
		vm.Leaks = append(vm.Leaks, site)
	}
	for _, s := range imp.LockSites {
		site := pages.LockSiteViewModel{Function: s.Function, Location: s.Location, Share: fmt.Sprintf("%.0f%%", s.Share*100), Count: s.Count, Average: duration(s.Mean), Max: duration(s.Max)}
		if len(s.Holders) > 0 {
			hd := s.Holders[0]
			site.HolderFunction, site.HolderLocation, site.HolderURL, site.HolderLabel = hd.Function, hd.Location, goroutineURL(hd.G, hd.WaitFrom, hd.WaitTo), gid(hd.G)
		}
		vm.Locks = append(vm.Locks, site)
	}
	vm.ImpactsTitle = impactsTitle(kind)
	for _, g := range imp.Goroutines {
		d := g.Detail
		m := pages.ImpactViewModel{Goroutine: gid(d.ID), Entry: d.Entry, Function: d.StoppedAt.Function, Location: d.StoppedAt.Location(), Reason: d.StoppedReason, URL: goroutineURL(d.ID, d.From, d.To)}
		if d.Agent {
			m.Entry += " (gorviz agent)"
		}
		m.Duration = duration(g.Duration)
		if g.Duration == 0 {
			m.Duration = string(d.MainCause)
		}
		if g.Note != "" && m.Reason == "" {
			m.Reason = g.Note
		}
		vm.Impacts = append(vm.Impacts, m)
	}
}

func impactsTitle(k domain.IssueKind) string {
	switch k {
	case domain.IssueGoroutineLeak:
		return "Leaked goroutines, one per stack"
	case domain.IssueCPUContention:
		return "Goroutines that were RUNNABLE the longest"
	case domain.IssueGCPressure:
		return "Goroutines made to do the most mark assist"
	case domain.IssueLockContention:
		return "Goroutines that waited on locks the longest"
	case domain.IssueLongSTW:
		return "Goroutines that stopped the world"
	case domain.IssueThreadExplosion:
		return "Goroutines that held OS threads in syscalls the longest"
	}
	return "Goroutines affected the most"
}

// occurrenceChart は期間を区切り、区切りごとに始まった発生の数
func occurrenceChart(starts []time.Duration, tr timeRange) components.ChartViewModel {
	const buckets = 48
	span := max(tr.To-tr.From, time.Second)
	step := max(span/buckets, time.Second)
	n := int((span + step - 1) / step)
	times := make([]time.Duration, n)
	v := make([]float64, n)
	for i := range times {
		times[i] = tr.From + step*time.Duration(i)
	}
	for _, t := range starts {
		if t >= tr.From && t <= tr.To {
			v[min(int((t-tr.From)/step), n-1)]++
		}
	}
	return buildChart(chartSpec{from: tr.From, to: tr.From + span, step: step, times: times, format: count, threshold: math.NaN(), zoom: true,
		series: []series{{label: "occurrences", class: "series-level-error", bar: true, values: v}}})
}
