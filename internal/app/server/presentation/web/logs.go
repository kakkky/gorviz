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
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
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
	keepFilter := url.Values{}
	for _, k := range []string{"level", "instance", "attr", "q"} {
		if vs := q[k]; len(vs) > 0 {
			keepFilter[k] = vs
		}
	}
	vm := pages.LogsViewModel{Base: base, Picker: picker(action, keepFilter, tr, found.From, found.To, logsPresets, logsDefault), Total: counts.Total,
		Chart: logHistogram(s, counts, step, q), Search: logsSearch(action, q, svc)}
	if tr.From.Before(found.RetainedSince) {
		vm.Retention = &components.UnavailableViewModel{Field: "logs before " + dateTime(found.RetainedSince), Text: pastRetention, Since: dateTime(found.RetainedSince)}
	}
	open := q.Get("log")
	for _, l := range found.Logs {
		id := l.ID()
		row := pages.LogRowViewModel{ID: "log-" + logAnchor(id), At: clockMilli(l.At), Instance: string(l.Instance), Level: levelText(l.Level), LevelClass: levelClass(l.Level),
			Message: l.Message, Exact: l.HasTraceSeq, OpenURL: servicePath(s, "logs/"+url.PathEscape(string(id))+"/goroutine"), Raw: l.Raw, Open: string(id) == open}
		if l.HasDuration {
			row.Duration = duration(l.Duration)
		}
		attrs := l.Attrs()
		keys := make([]string, 0, len(attrs))
		for k := range attrs {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			row.Fields = append(row.Fields, pages.KeyValueViewModel{Key: k, Value: attrs[k]})
		}
		if row.Open {
			d, err := h.detail(r, s, id, now)
			if err != nil {
				middleware.SetError(r, err)
				return
			}
			row.Detail = &d
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

func logsSearch(action string, q url.Values, svc usecase.ServiceSummary) components.SearchFormViewModel {
	vm := components.SearchFormViewModel{Action: action, Query: q.Get("q"), Placeholder: "search text (e.g. a trace_id from your APM)"}
	attr := ""
	if vs := slices.DeleteFunc(slices.Clone(q["attr"]), func(a string) bool { return a == "" }); len(vs) > 0 {
		attr = vs[0]
	}
	vm.Inputs = []components.SearchInputViewModel{{Name: "attr", Value: attr, Placeholder: "key=value", Title: "match a JSON key and value of the line (e.g. path=/todos)"}}
	levels := components.SearchSelectViewModel{Name: "level", All: "all levels", Selected: q.Get("level")}
	for _, l := range []observation.LogLevel{observation.LogLevelError, observation.LogLevelWarn, observation.LogLevelInfo, observation.LogLevelDebug} {
		levels.Options = append(levels.Options, components.SearchOptionViewModel{Value: string(l), Label: string(l) + " and above"})
	}
	instances := components.SearchSelectViewModel{Name: "instance", All: "all instances", Selected: q.Get("instance")}
	for _, i := range svc.Instances {
		instances.Options = append(instances.Options, components.SearchOptionViewModel{Value: string(i.Name), Label: string(i.Name)})
	}
	vm.Selects = []components.SearchSelectViewModel{levels, instances}
	for k, vs := range timeQuery(q) {
		vm.Keep = append(vm.Keep, components.KeptParamViewModel{Name: k, Value: vs[0]})
	}
	return vm
}

// logAnchor は行の id 属性に使える形にする
func logAnchor(id domain.LogID) string {
	return strings.NewReplacer("~", "-", ".", "-", ":", "-").Replace(string(id))
}

func levelText(l observation.LogLevel) string {
	if l == observation.LogLevelUnknown {
		return "-"
	}
	return string(l)
}

func levelClass(l observation.LogLevel) string {
	if l == observation.LogLevelUnknown {
		return "unknown"
	}
	return strings.ToLower(string(l))
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
	for i, b := range counts.Buckets {
		times[i] = b.At
	}
	var ss []series
	for _, l := range []struct {
		level observation.LogLevel
		label string
	}{{observation.LogLevelError, "ERROR"}, {observation.LogLevelWarn, "WARN"}, {observation.LogLevelInfo, "INFO"}, {observation.LogLevelDebug, "DEBUG"}, {observation.LogLevelUnknown, "no level"}} {
		v := make([]float64, n)
		for i, b := range counts.Buckets {
			v[i] = float64(b.Counts[string(l.level)])
		}
		ss = append(ss, series{label: l.label, class: "series-level-" + levelClass(l.level), bar: true, stack: true, values: v})
	}
	return buildChart(chartSpec{title: "Matching lines per " + duration(step), from: counts.From, to: counts.To, step: step, times: times, format: count, threshold: math.NaN(), zoom: true,
		series: ss,
		hoverURL: func(i int) string {
			bq := cloneQuery(q)
			bq.Del("cursor")
			bq.Del("log")
			bq.Set("from", rfc(times[i]))
			bq.Set("to", rfc(times[i].Add(step)))
			return withQuery(servicePath(s, "logs"), bq)
		}})
}

func (h *GetLogsHandler) detail(r *http.Request, s observation.ServiceName, id domain.LogID, now time.Time) (pages.LogDetailViewModel, error) {
	out, err := h.log.Exec(r.Context(), usecase.GetLogUsecaseInput{Service: s, Log: id, Now: now})
	if err != nil {
		return pages.LogDetailViewModel{}, err
	}
	l := out.Log
	vm := pages.LogDetailViewModel{At: dateTimeMilli(l.At), Source: string(l.Source), Period: span(out.Period.From, out.Period.To), PeriodNote: "around the time it was written"}
	if !l.TimeFromLog {
		vm.TimeNote = "the line had no time; this is when the agent read it"
	}
	if l.HasDuration {
		vm.PeriodNote = "the request it ended (took " + duration(l.Duration) + ")"
	}
	vm.Writer = logWriter(s, out)
	for _, p := range out.Preserved {
		vm.Preserved = append(vm.Preserved, components.LinkItemViewModel{Label: span(p.From, p.To), URL: paths.Scheduler(s, l.Instance, p.From, p.To), Note: "kept around error logs like this one"})
	}
	return vm, nil
}

func logWriter(s observation.ServiceName, out usecase.GetLogUsecaseOutput) pages.LogWriterViewModel {
	l := out.Log
	w := pages.LogWriterViewModel{SchedulerURL: paths.Scheduler(s, l.Instance, out.Period.From, out.Period.To)}
	switch out.WriterStatus {
	case usecase.WriterFound:
		g := out.Writer.Goroutine
		w.Found, w.Goroutine = true, "G"+itoa(int(g))
		w.Certainty, w.CertaintyNote = string(out.Writer.Certainty), writerCertaintyNote(out.Writer.Certainty)
		w.URL = goroutineFromLog(s, l.Instance, g, l.ID())
	case usecase.WriterUnknown:
		w.Note = "not told: the logger adapter numbered no line and more than one goroutine was running at that time"
	case usecase.WriterPending:
		w.Note = "not matched yet; it is matched once the execution trace of that time arrives"
	case usecase.WriterPastRetention:
		w.Unavailable = &components.UnavailableViewModel{Field: "writer goroutine", Text: pastRetention}
	}
	return w
}

// GetLogGoroutineHandler は一覧の行の Goroutine →。書いた goroutine が分かればその診断へ、分からなければその時刻の Scheduler へ移す
type GetLogGoroutineHandler struct {
	log *usecase.GetLogUsecase
}

func NewGetLogGoroutineHandler(log *usecase.GetLogUsecase) *GetLogGoroutineHandler {
	return &GetLogGoroutineHandler{log: log}
}

func (h *GetLogGoroutineHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	s := observation.ServiceName(r.PathValue("service"))
	out, err := h.log.Exec(r.Context(), usecase.GetLogUsecaseInput{Service: s, Log: domain.LogID(r.PathValue("log")), Now: time.Now()})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	w := logWriter(s, out)
	to := w.SchedulerURL
	if w.Found {
		to = w.URL
	}
	http.Redirect(rw, r, to, http.StatusSeeOther)
}

func writerCertaintyNote(c domain.Certainty) string {
	switch c {
	case domain.CertaintyFact:
		return "FACT: matched by the number the logger adapter wrote"
	case domain.CertaintyInference:
		return "INFERENCE: the only goroutine running at that time"
	}
	return ""
}
