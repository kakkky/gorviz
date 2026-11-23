package web

import (
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/layout"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const issuesDefault = "-744h"

var issuesPresets = []preset{{"last 1h", "-1h"}, {"last 24h", "-24h"}, {"last 7d", "-168h"}, {"last 31d", issuesDefault}}

type GetIssuesHandler struct {
	nav    navigator
	issues *usecase.ListIssuesUsecase
}

func NewGetIssuesHandler(services *usecase.ListServicesUsecase, issues *usecase.ListIssuesUsecase) *GetIssuesHandler {
	return &GetIssuesHandler{nav: navigator{services: services}, issues: issues}
}

func (h *GetIssuesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s := observation.ServiceName(r.PathValue("service"))
	q := r.URL.Query()
	tr, err := parseRange(q, now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	base, _, err := h.nav.base(r.Context(), "Issues", s, "", tabIssues)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	out, err := h.issues.Exec(r.Context(), usecase.ListIssuesUsecaseInput{Service: s, From: tr.From, To: tr.To, Cursor: domain.PageCursor(q.Get("cursor")), Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	action := servicePath(s, "issues")
	vm := pages.IssuesViewModel{Base: base, Picker: picker(action, nil, tr, out.From, out.To, issuesPresets, issuesDefault),
		Retention: "Issues are kept since " + dateTime(out.RetainedSince) + "."}
	for _, is := range out.Issues {
		vm.Rows = append(vm.Rows, pages.IssueRowViewModel{Kind: string(is.Kind), Subject: is.Subject, Certainty: string(is.Certainty), CertaintyNote: certaintyNote(is.Certainty),
			Occurrences: is.Occurrences, Last: dateTime(is.LastOccurredAt), Ongoing: is.Ongoing, Instances: instanceNames(is.OngoingInstances), URL: issueURL(s, is.Name, tr)})
	}
	if out.Next != "" {
		nq := url.Values{}
		for k, v := range q {
			nq[k] = v
		}
		nq.Set("cursor", string(out.Next))
		vm.NextURL = withQuery(action, nq)
	}
	render(rw, r, pages.Issues(vm))
}

func instanceNames(is []observation.InstanceName) string {
	names := make([]string, 0, len(is))
	for _, i := range is {
		names = append(names, string(i))
	}
	return strings.Join(names, ", ")
}

// issueURL は Issue の画面。一覧で選んでいた期間を引き継ぐ
func issueURL(s observation.ServiceName, name string, tr timeRange) string {
	q := url.Values{}
	if tr.FromRaw != "" {
		q.Set("from", tr.FromRaw)
	}
	if tr.ToRaw != "" {
		q.Set("to", tr.ToRaw)
	}
	return withQuery(paths.Issue(s, name), q)
}

type GetIssueHandler struct {
	nav   navigator
	issue *usecase.GetIssueUsecase
}

func NewGetIssueHandler(services *usecase.ListServicesUsecase, issue *usecase.GetIssueUsecase) *GetIssueHandler {
	return &GetIssueHandler{nav: navigator{services: services}, issue: issue}
}

func (h *GetIssueHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s, name := observation.ServiceName(r.PathValue("service")), r.PathValue("issue")
	tr, err := parseRange(r.URL.Query(), now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	base, _, err := h.nav.base(r.Context(), name, s, "", tabIssues)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	out, err := h.issue.Exec(r.Context(), usecase.GetIssueUsecaseInput{Service: s, Issue: name, From: tr.From, To: tr.To, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	render(rw, r, pages.Issue(issueView(base, s, tr, out, now)))
}

func issueView(base layout.BaseViewModel, s observation.ServiceName, tr timeRange, out usecase.GetIssueUsecaseOutput, now time.Time) pages.IssueViewModel {
	sum := out.Summary
	from := tr.From
	if from.IsZero() {
		from = out.RetainedSince
	}
	to := tr.To
	if to.IsZero() {
		to = now
	}
	vm := pages.IssueViewModel{Base: base, Picker: picker(paths.Issue(s, sum.Name), nil, tr, from, to, issuesPresets, issuesDefault),
		Kind: string(sum.Kind), Subject: sum.Subject, Certainty: string(sum.Certainty), CertaintyNote: certaintyNote(sum.Certainty),
		Ongoing: sum.Ongoing, Instances: instanceNames(sum.OngoingInstances), NextAction: sum.NextAction,
		Counts:    issueCounts(out.CountsByDay),
		Retention: "Issues are kept since " + dateTime(out.RetainedSince) + "."}
	occs := slices.Clone(out.Occurrences)
	slices.Reverse(occs)
	for i, o := range occs {
		vm.Occurrences = append(vm.Occurrences, occurrenceView(s, sum, o, i == 0))
	}
	return vm
}

func issueCounts(days []usecase.DayCount) components.ChartViewModel {
	if len(days) == 0 {
		return buildChart(chartSpec{title: "Occurrences per day", from: time.Now().Add(-24 * time.Hour), to: time.Now(), step: 24 * time.Hour, format: count, threshold: math.NaN()})
	}
	days = slices.Clone(days)
	slices.SortFunc(days, func(a, b usecase.DayCount) int { return a.Day.Compare(b.Day) })
	times := make([]time.Time, len(days))
	values := make([]float64, len(days))
	for i, d := range days {
		times[i], values[i] = d.Day, float64(d.Count)
	}
	from := days[0].Day
	to := days[len(days)-1].Day.Add(24 * time.Hour)
	if to.Sub(from) < 7*24*time.Hour {
		from = to.Add(-7 * 24 * time.Hour)
	}
	return buildChart(chartSpec{title: "Occurrences per day", from: from, to: to, step: 24 * time.Hour, times: times, format: count, threshold: math.NaN(),
		series: []series{{label: "occurrences", class: "s-error", bar: true, values: values}}})
}

var impactTitles = map[domain.IssueKind]string{
	domain.IssueGoroutineLeak:   "Leaked goroutines",
	domain.IssueCPUContention:   "Goroutines that were RUNNABLE the longest",
	domain.IssueGCPressure:      "Goroutines made to do the most mark assist",
	domain.IssueLockContention:  "Goroutines that waited on locks the longest",
	domain.IssueLongSTW:         "Goroutines that stopped the world",
	domain.IssueThreadExplosion: "Goroutines that held OS threads in syscall or cgo the longest",
}

func occurrenceView(s observation.ServiceName, sum usecase.IssueSummary, o usecase.OccurrenceDetail, open bool) pages.OccurrenceViewModel {
	from, to := o.Around()
	end := o.EndedAt
	period := dateTime(o.StartedAt) + " → ongoing"
	if !o.Ongoing() {
		period = span(o.StartedAt, end)
	}
	vm := pages.OccurrenceViewModel{Instance: string(o.Instance), Period: period, Ongoing: o.Ongoing(), Judge: string(o.Judge), Certainty: string(o.Certainty),
		CertaintyNote: certaintyNote(o.Certainty), Reason: o.Reason, SchedulerURL: paths.Scheduler(s, o.Instance, from, to), ImpactsTitle: impactTitles[sum.Kind], Open: open}
	switch {
	case o.TraceFile == "":
		vm.Trace = &components.UnavailableViewModel{Field: "execution trace around it", Text: "前後の execution trace はまだ残していないか、残せなかった。Scheduler では残っている trace から再生する"}
	case !o.TraceRetained:
		vm.Trace = &components.UnavailableViewModel{Field: "execution trace around it", Text: pastRetention}
	}
	goroutineURL := func(g observation.GoroutineID) string {
		if g == 0 {
			return ""
		}
		return goroutineFromIssue(s, o.Instance, g, sum.Name, o.Occurrence)
	}
	for _, m := range o.Sites {
		switch sum.Kind {
		case domain.IssueGoroutineLeak:
			first := m.Goroutine
			if first == 0 && len(m.Goroutines) > 0 {
				first = m.Goroutines[0]
			}
			l := pages.LeakSiteViewModel{CreatedBy: m.CreatedBy, CreatedByLocation: m.CreatedByLocation, Function: m.Function, Location: m.Location, Reason: m.Reason,
				Certainty: string(m.Certainty), CertaintyNote: leakCertaintyNote(m.Certainty), URL: goroutineURL(first), Goroutines: goroutineList(m.Goroutines)}
			if m.Of > 0 {
				l.Leak = "同じ場所で待っている " + itoa(m.Of) + " 個のうち " + itoa(m.Count) + " 個がリーク"
			} else if m.Count > 0 {
				l.Leak = itoa(m.Count) + " 個がリーク"
			}
			vm.Leaks = append(vm.Leaks, l)
		case domain.IssueLockContention:
			l := pages.LockSiteViewModel{Function: m.Function, Location: m.Location, Share: percent(m.Share), Count: m.Count, Max: duration(m.Max),
				HolderFunction: m.HolderFunction, HolderLocation: m.HolderLocation, HolderURL: goroutineURL(m.HolderGoroutine)}
			if m.Count > 0 {
				l.Average = duration(m.Duration / time.Duration(m.Count))
			}
			if m.HolderGoroutine != 0 {
				l.HolderLabel = "G" + itoa(int(m.HolderGoroutine))
			}
			vm.Locks = append(vm.Locks, l)
		}
	}
	for _, m := range o.Impacts {
		iv := pages.ImpactViewModel{Entry: m.Entry, Function: m.Function, Location: m.Location, Reason: m.Reason, Duration: duration(m.Duration), URL: goroutineURL(m.Goroutine)}
		switch {
		case m.Goroutine != 0:
			iv.Goroutine = "G" + itoa(int(m.Goroutine))
		case len(m.Goroutines) > 0:
			iv.Goroutine, iv.URL = goroutineList(m.Goroutines), goroutineURL(m.Goroutines[0])
		}
		if m.Count > 0 && m.Duration == 0 {
			iv.Duration = itoa(m.Count) + " goroutines"
		}
		vm.Impacts = append(vm.Impacts, iv)
	}
	return vm
}

func goroutineList(gs []observation.GoroutineID) string {
	var b strings.Builder
	for i, g := range gs {
		if i == 5 {
			b.WriteString(" …")
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("G" + itoa(int(g)))
	}
	return b.String()
}

func leakCertaintyNote(c domain.Certainty) string {
	switch c {
	case domain.CertaintyFact:
		return "FACT: 同じスタックの goroutine がちょうどリークの件数だけあった"
	case domain.CertaintyInference:
		return "INFERENCE: 同じスタックの goroutine のうち、待っている時間が長い順に件数分を選んだ"
	}
	return ""
}
