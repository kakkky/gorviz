package web

import (
	"math"
	"net/http"
	"net/url"
	"slices"
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

const logsDefault = "-15m"

var logsPresets = []preset{{"last 5m", "-5m"}, {"last 15m", "-15m"}, {"last 1h", "-1h"}, {"last 6h", "-6h"}, {"last 24h", "-24h"}}

// GetLogsHandler は Logs の画面。log を渡されたらその行を開き、行を開く turbo-frame の要求にはその行だけを返す
type GetLogsHandler struct {
	nav    navigator
	search *usecase.SearchLogsUsecase
	counts *usecase.CountLogsUsecase
	log    *usecase.GetLogUsecase
}

func NewGetLogsHandler(services *usecase.ListServicesUsecase, search *usecase.SearchLogsUsecase, counts *usecase.CountLogsUsecase, log *usecase.GetLogUsecase) *GetLogsHandler {
	return &GetLogsHandler{nav: navigator{services: services}, search: search, counts: counts, log: log}
}

// histogramStep は件数の推移の区切り。60 本ほどになる切りのよい長さ
func histogramStep(span time.Duration) time.Duration {
	for _, s := range []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour} {
		if span/s <= 60 {
			return s
		}
	}
	return 24 * time.Hour
}

func logsRange(q url.Values, now time.Time) (timeRange, error) {
	tr, err := parseRange(q, now)
	if err != nil {
		return timeRange{}, err
	}
	if tr.FromRaw == "" && tr.ToRaw == "" {
		tr.FromRaw, tr.From = logsDefault, now.Add(-15*time.Minute)
	}
	if tr.To.IsZero() {
		tr.To = now
	}
	if tr.From.IsZero() {
		tr.From = tr.To.Add(-time.Hour)
	}
	return tr, nil
}

func (h *GetLogsHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s := observation.ServiceName(r.PathValue("service"))
	q := r.URL.Query()
	action := servicePath(s, "logs")
	if turbo.FrameID(r) == partials.LogDetailFrame {
		detail, err := h.detail(r, s, q, action, now)
		if err != nil {
			middleware.SetError(r, err)
			return
		}
		render(rw, r, partials.LogDetail(detail))
		return
	}
	tr, err := logsRange(q, now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	filter, err := parseLogFilter(q)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	base, svc, err := h.nav.base(r.Context(), "Logs", s, "", tabLogs)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	step := histogramStep(tr.To.Sub(tr.From))
	counts, err := h.counts.Exec(r.Context(), usecase.CountLogsUsecaseInput{Service: s, From: tr.From.Truncate(step), To: tr.To, Filter: filter, By: "level", Step: step, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	found, err := h.search.Exec(r.Context(), usecase.SearchLogsUsecaseInput{Service: s, From: tr.From, To: tr.To, Filter: filter, After: domain.LogID(q.Get("cursor")), Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	detail, err := h.detail(r, s, q, action, now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	keepFilter := url.Values{}
	for _, k := range []string{"level", "instance", "attr", "q"} {
		if vs := q[k]; len(vs) > 0 {
			keepFilter[k] = vs
		}
	}
	vm := pages.LogsViewModel{Base: base, Picker: picker(action, keepFilter, tr, found.From, found.To, logsPresets, logsDefault), Total: counts.Total,
		Histogram: logHistogram(s, counts, step, q), Detail: detail}
	vm.Filter = pages.LogFilterViewModel{Action: action, Level: string(filter.Level), Instance: string(filter.Instance), Text: filter.Text,
		Attrs: append(slices.DeleteFunc(slices.Clone(q["attr"]), func(a string) bool { return a == "" }), ""), ClearURL: withQuery(action, timeQuery(q))}
	for k, vs := range timeQuery(q) {
		vm.Filter.Keep = append(vm.Filter.Keep, components.KeptParamViewModel{Name: k, Value: vs[0]})
	}
	for _, i := range svc.Instances {
		vm.Filter.Instances = append(vm.Filter.Instances, string(i.Name))
	}
	for _, c := range counts.Counts {
		vm.Levels = append(vm.Levels, pages.LevelCountViewModel{Level: c.Value, Count: c.Count})
	}
	if tr.From.Before(found.RetainedSince) {
		vm.Retention = &components.UnavailableViewModel{Field: "logs before " + dateTime(found.RetainedSince), Text: pastRetention, Since: dateTime(found.RetainedSince)}
	}
	open := q.Get("log")
	for _, l := range found.Logs {
		rq := cloneQuery(q)
		rq.Set("log", string(l.ID()))
		row := pages.LogRowViewModel{At: clockMilli(l.At), Instance: string(l.Instance), Level: string(l.Level), Message: l.Message, URL: withQuery(action, rq), Open: string(l.ID()) == open}
		if l.HasDuration {
			row.Duration = duration(l.Duration)
		}
		vm.Rows = append(vm.Rows, row)
	}
	if found.Next != "" {
		nq := cloneQuery(q)
		nq.Set("cursor", string(found.Next))
		nq.Del("log")
		vm.NextURL = withQuery(action, nq)
	}
	render(rw, r, pages.Logs(vm))
}

func cloneQuery(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = slices.Clone(v)
	}
	return out
}

func timeQuery(q url.Values) url.Values {
	out := url.Values{}
	for _, k := range []string{"from", "to"} {
		if v := q.Get(k); v != "" {
			out.Set(k, v)
		}
	}
	return out
}

func logHistogram(s observation.ServiceName, counts usecase.CountLogsUsecaseOutput, step time.Duration, q url.Values) components.ChartViewModel {
	n := len(counts.Buckets)
	times := make([]time.Time, n)
	all, errs := make([]float64, n), make([]float64, n)
	for i, b := range counts.Buckets {
		times[i], all[i], errs[i] = b.At, float64(b.Total), float64(b.Counts[string(observation.LogLevelError)])
	}
	return buildChart(chartSpec{title: "Matching lines per " + duration(step), from: counts.From, to: counts.To, step: step, times: times, format: count, threshold: math.NaN(),
		series: []series{{label: "lines", class: "s-muted", bar: true, values: all}, {label: "errors", class: "s-error", bar: true, values: errs}},
		hoverURL: func(i int) string {
			bq := cloneQuery(q)
			bq.Del("cursor")
			bq.Del("log")
			bq.Set("from", rfc(times[i]))
			bq.Set("to", rfc(times[i].Add(step)))
			return withQuery(servicePath(s, "logs"), bq)
		}})
}

func (h *GetLogsHandler) detail(r *http.Request, s observation.ServiceName, q url.Values, action string, now time.Time) (partials.LogDetailViewModel, error) {
	id := domain.LogID(q.Get("log"))
	if id == "" {
		return partials.LogDetailViewModel{}, nil
	}
	out, err := h.log.Exec(r.Context(), usecase.GetLogUsecaseInput{Service: s, Log: id, Now: now})
	if err != nil {
		return partials.LogDetailViewModel{}, err
	}
	l := out.Log
	cq := cloneQuery(q)
	cq.Del("log")
	vm := partials.LogDetailViewModel{Open: true, At: dateTimeMilli(l.At), Instance: string(l.Instance), Level: string(l.Level), Message: l.Message, Source: string(l.Source),
		Raw: l.Raw, Period: span(out.Period.From, out.Period.To), CloseURL: withQuery(action, cq)}
	if !l.TimeFromLog {
		vm.TimeNote = "the line had no time; this is when the agent read it"
	}
	vm.PeriodNote = "around the time it was written"
	if l.HasDuration {
		vm.Duration = duration(l.Duration)
		vm.PeriodNote = "the request it ended (took " + vm.Duration + ")"
	}
	keys := make([]string, 0, len(out.Attrs))
	for k := range out.Attrs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		vm.Attrs = append(vm.Attrs, partials.KeyValueViewModel{Key: k, Value: out.Attrs[k]})
	}
	vm.Writer.SchedulerURL = paths.Scheduler(s, l.Instance, out.Period.From, out.Period.To)
	switch out.WriterStatus {
	case usecase.WriterFound:
		g := out.Writer.Goroutine
		vm.Writer.Found, vm.Writer.Goroutine = true, "G"+itoa(int(g))
		vm.Writer.Certainty, vm.Writer.CertaintyNote = string(out.Writer.Certainty), writerCertaintyNote(out.Writer.Certainty)
		vm.Writer.URL = goroutineFromLog(s, l.Instance, g, l.ID())
	case usecase.WriterUnknown:
		vm.Writer.Note = "The goroutine that wrote it could not be told: the logger adapter numbered no line and more than one goroutine was running at that time."
	case usecase.WriterPending:
		vm.Writer.Note = "The writer is not matched yet; it is matched once the execution trace of that time arrives."
	case usecase.WriterPastRetention:
		vm.Writer.Unavailable = &components.UnavailableViewModel{Field: "writer goroutine", Text: pastRetention}
	}
	for _, p := range out.Preserved {
		vm.Preserved = append(vm.Preserved, components.LinkItemViewModel{Label: span(p.From, p.To), URL: paths.Scheduler(s, l.Instance, p.From, p.To), Note: "kept around error logs like this one"})
	}
	return vm, nil
}

func writerCertaintyNote(c domain.Certainty) string {
	switch c {
	case domain.CertaintyFact:
		return "FACT: ロガーアダプタが振った番号で結んだ"
	case domain.CertaintyInference:
		return "INFERENCE: その時刻に走っていた goroutine が 1 つだけ"
	}
	return ""
}
