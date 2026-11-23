package web

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const overviewDefault = "-1h"

var overviewPresets = []preset{{"last 15m", "-15m"}, {"last 1h", "-1h"}, {"last 6h", "-6h"}, {"last 24h", "-24h"}, {"last 7d", "-168h"}}

type GetOverviewHandler struct {
	nav      navigator
	overview *usecase.GetOverviewUsecase
}

func NewGetOverviewHandler(services *usecase.ListServicesUsecase, overview *usecase.GetOverviewUsecase) *GetOverviewHandler {
	return &GetOverviewHandler{nav: navigator{services: services}, overview: overview}
}

func overviewRange(q url.Values, now time.Time) (timeRange, error) {
	r, err := parseRange(q, now)
	if err != nil {
		return timeRange{}, err
	}
	if r.FromRaw == "" && r.ToRaw == "" {
		r.FromRaw, r.From = overviewDefault, now.Add(-time.Hour)
	}
	return r, nil
}

func (h *GetOverviewHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s := observation.ServiceName(r.PathValue("service"))
	tr, err := overviewRange(r.URL.Query(), now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	base, svc, err := h.nav.base(r.Context(), "Overview", s, "", tabOverview)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	out, err := h.overview.Exec(r.Context(), usecase.GetOverviewUsecaseInput{Service: s, From: tr.From, To: tr.To, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	vm := pages.OverviewViewModel{Base: base, Body: overviewBody(out, svc, now),
		Picker: picker(paths.Overview(s), nil, tr, out.From, out.To, overviewPresets, overviewDefault)}
	if tr.Live() {
		vm.StreamURL = withQuery(servicePath(s, "overview/stream"), url.Values{"from": {tr.FromRaw}})
	}
	render(rw, r, pages.Overview(vm))
}

// GetOverviewStreamHandler は最新を見ている Overview に、区切り (10 秒) ごとに描き直した中身を SSE の turbo-stream で送る
type GetOverviewStreamHandler struct {
	nav      navigator
	overview *usecase.GetOverviewUsecase
}

func NewGetOverviewStreamHandler(services *usecase.ListServicesUsecase, overview *usecase.GetOverviewUsecase) *GetOverviewStreamHandler {
	return &GetOverviewStreamHandler{nav: navigator{services: services}, overview: overview}
}

func (h *GetOverviewStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	s := observation.ServiceName(r.PathValue("service"))
	q := r.URL.Query()
	if _, err := overviewRange(q, time.Now()); err != nil {
		middleware.SetError(r, err)
		return
	}
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		middleware.SetError(r, domain.ErrInternal.Wrap(err, "open the overview stream"))
		return
	}
	streamEvery(r.Context(), domain.MetricStep, func(ctx context.Context) error {
		now := time.Now()
		tr, _ := overviewRange(q, now)
		_, svc, err := h.nav.base(ctx, "Overview", s, "", tabOverview)
		if err != nil {
			return err
		}
		out, err := h.overview.Exec(ctx, usecase.GetOverviewUsecaseInput{Service: s, From: tr.From, To: tr.To, Now: now})
		if err != nil {
			return err
		}
		return sse.Write(ctx, partials.OverviewStream(overviewBody(out, svc, now)))
	})
}

func overviewBody(out usecase.GetOverviewUsecaseOutput, svc usecase.ServiceSummary, now time.Time) partials.OverviewBodyViewModel {
	s := out.Service
	vm := partials.OverviewBodyViewModel{Range: span(out.From, out.To), UpdatedAt: clock(now), IssuesURL: servicePath(s, "issues")}
	for _, is := range out.OngoingIssues {
		var inst []string
		for _, i := range is.OngoingInstances {
			inst = append(inst, string(i))
		}
		vm.Ongoing = append(vm.Ongoing, partials.OngoingIssueViewModel{Kind: string(is.Kind), Subject: is.Subject, Certainty: string(is.Certainty), CertaintyNote: certaintyNote(is.Certainty),
			Instances: strings.Join(inst, ", "), Last: dateTime(is.LastOccurredAt), URL: paths.Issue(s, is.Name)})
	}
	if out.From.Before(out.RetainedSince) {
		vm.Retention = &components.UnavailableViewModel{Field: "metrics before " + dateTime(out.RetainedSince), Text: pastRetention, Since: dateTime(out.RetainedSince)}
	}
	var marks []chartMark
	for _, e := range out.Lifecycle {
		m := chartMark{at: e.At, class: "start", title: string(e.Instance) + " started " + dateTime(e.At)}
		switch {
		case e.Kind == domain.LifecycleEnd:
			m.class, m.title = "end", string(e.Instance)+" ended "+dateTime(e.At)
		case e.Restart:
			m.class, m.title = "restart", string(e.Instance)+" restarted "+dateTime(e.At)
		}
		marks = append(marks, m)
		vm.Lifecycle = append(vm.Lifecycle, dateTime(e.At)+"  "+m.title)
	}
	vm.Charts = overviewCharts(out, marks)
	for _, i := range svc.Instances {
		state := "ended"
		if i.Running {
			state = "running"
		}
		if n := len(i.Incarnations); n > 0 {
			last := i.Incarnations[n-1]
			if !last.StartedAt.IsZero() {
				state += " · started " + dateTime(last.StartedAt)
			}
			if last.Ended() {
				state += " · ended " + dateTime(last.EndedAt)
			}
			if restarts := countRestarts(i.Incarnations); restarts > 0 {
				state += " · restarted " + itoa(restarts) + " times"
			}
		}
		vm.Instances = append(vm.Instances, partials.OverviewInstanceViewModel{Name: string(i.Name), Running: i.Running, State: state,
			SchedulerURL: paths.Scheduler(s, i.Name, time.Time{}, time.Time{}), LogsURL: withQuery(servicePath(s, "logs"), url.Values{"instance": {string(i.Name)}})})
	}
	return vm
}

func countRestarts(incs []domain.Incarnation) int {
	n := 0
	for _, i := range incs {
		if i.Restart {
			n++
		}
	}
	return n
}

func overviewCharts(out usecase.GetOverviewUsecaseOutput, marks []chartMark) []components.ChartViewModel {
	n := len(out.Points)
	times := make([]time.Time, n)
	vals := func(f func(m domain.MetricSample) (float64, bool)) []float64 {
		v := make([]float64, n)
		for i, p := range out.Points {
			if x, ok := f(p); ok {
				v[i] = x
			} else {
				v[i] = math.NaN()
			}
		}
		return v
	}
	for i, p := range out.Points {
		times[i] = p.At
	}
	traced := func(f func(m domain.MetricSample) float64) []float64 {
		return vals(func(m domain.MetricSample) (float64, bool) { return f(m), m.Traced > 0 })
	}
	spec := func(title string, format func(float64) string, ss ...series) chartSpec {
		return chartSpec{title: title, from: out.From, to: out.To, step: out.Step, times: times, series: ss, format: format, threshold: math.NaN(), marks: marks}
	}
	logsURL := func(level string) func(i int) string {
		return func(i int) string {
			q := rangeQuery(times[i], times[i].Add(out.Step))
			if level != "" {
				q.Set("level", level)
			}
			return withQuery(servicePath(out.Service, "logs"), q)
		}
	}

	goroutines := spec("Goroutines", count, series{label: "goroutines", class: "s1", values: vals(func(m domain.MetricSample) (float64, bool) { return float64(m.Goroutines), m.HasGoroutines })})
	goroutines.note = "summed over instances"

	errors := spec("Logs and errors", count,
		series{label: "logs", class: "s-muted", bar: true, values: vals(func(m domain.MetricSample) (float64, bool) { return float64(m.Logs), true })},
		series{label: "errors", class: "s-error", bar: true, values: vals(func(m domain.MetricSample) (float64, bool) { return float64(m.Errors), true })})
	errors.hoverURL = logsURL("")
	errors.note = "click a bar to open the logs of that time"

	cpu := spec("CPU wait: RUNNABLE → RUNNING p99", func(v float64) string { return duration(time.Duration(v)) },
		series{label: "p99", class: "s2", values: vals(func(m domain.MetricSample) (float64, bool) {
			d, ok := m.SchedP99()
			return float64(d), ok && m.Traced > 0
		})})
	cpu.threshold, cpu.thLabel = float64(time.Millisecond), "CPU_CONTENTION: p99 ≥ 1ms"

	runnable := spec("Share of goroutine time spent RUNNABLE", percent,
		series{label: "runnable", class: "s2", values: traced(func(m domain.MetricSample) float64 { return m.RunnableShare() })})

	heap := spec("Heap", func(v float64) string { return bytesOf(v) },
		series{label: "heap", class: "s3", values: vals(func(m domain.MetricSample) (float64, bool) { return float64(m.HeapBytes), m.HeapBytes > 0 })},
		series{label: "goal", class: "s3", dashed: true, values: vals(func(m domain.MetricSample) (float64, bool) { return float64(m.HeapGoalBytes), m.HeapGoalBytes > 0 })})

	gc := spec("GC CPU share", percent,
		series{label: "GC", class: "s4", values: traced(func(m domain.MetricSample) float64 { return m.GCShare() })},
		series{label: "mark assist", class: "s5", values: traced(func(m domain.MetricSample) float64 { return m.MarkAssistShare() })})
	gc.threshold, gc.thLabel = 0.05, "GC_PRESSURE: mark assist > 5% (GC ≥ 50% is FACT)"

	lock := spec("Lock wait per running time", percent,
		series{label: "lock wait / running", class: "s6", values: vals(func(m domain.MetricSample) (float64, bool) { return m.LockWaitRatio(), m.Traced > 0 && m.Running > 0 })})
	lock.threshold, lock.thLabel = 1, "LOCK_CONTENTION: lock wait > running"

	threads := spec("OS threads", count, series{label: "threads", class: "s1", values: traced(func(m domain.MetricSample) float64 { return float64(m.Threads) })})
	threads.threshold, threads.thLabel = 5000, "THREAD_EXPLOSION: more than half of the 10,000 limit"

	out2 := []components.ChartViewModel{}
	for _, c := range []chartSpec{goroutines, errors, cpu, runnable, heap, gc, lock, threads} {
		out2 = append(out2, buildChart(c))
	}
	return out2
}
