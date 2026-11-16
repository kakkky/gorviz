package handler

import (
	"cmp"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const (
	overviewStreamURL    = "/overview/stream"
	overviewIssueRows    = 3
	overviewOriginsLimit = 10
	overviewOriginNotes  = 3
	overviewSeriesClass  = "series-"
)

// overviewRanges は Overview で選べる期間。最初のものを既定にはせず、overviewDefaultRange を使う
var overviewRanges = []struct {
	Key  string
	Span time.Duration
}{
	{"15m", 15 * time.Minute}, {"30m", 30 * time.Minute}, {"1h", time.Hour}, {"4h", 4 * time.Hour},
	{"1d", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour}, {"30d", 30 * 24 * time.Hour},
}

const overviewDefaultRange = "1h"

// overviewMinZoom より短くは拡大しない。集計の一番細かい区切り (10 秒) で 6 点
const overviewMinZoom = time.Minute

// overviewWindow は Overview で見る期間。To が空なら今まで (refresh ごとに右へ進む)、あれば拡大した [To-Span, To]
type overviewWindow struct {
	Key  string
	Span time.Duration
	To   time.Time
}

// overviewWindowOf は ?range= と、拡大したときの ?from=&to= (UNIX ミリ秒) を読む
func overviewWindowOf(r *http.Request) overviewWindow {
	key, span := overviewRange(r)
	w := overviewWindow{Key: key, Span: span}
	from, errFrom := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	to, errTo := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	if errFrom != nil || errTo != nil || to <= from {
		return w
	}
	w.To = time.UnixMilli(to)
	w.Span = min(max(time.Duration(to-from)*time.Millisecond, overviewMinZoom), overviewRanges[len(overviewRanges)-1].Span)
	return w
}

func (w overviewWindow) zoomed() bool {
	return !w.To.IsZero()
}

// bounds は今見る期間の終わりと長さ。拡大していなければ今までの Span。
// 拡大した終わりが未来 (ログの行の前後を開いたときなど) なら、始まりはそのままに今までにする
func (w overviewWindow) bounds(now time.Time) (time.Time, time.Duration) {
	if !w.zoomed() {
		return now, w.Span
	}
	if w.To.After(now) {
		return now, max(now.Sub(w.To.Add(-w.Span)), overviewMinZoom)
	}
	return w.To, w.Span
}

func (w overviewWindow) query() url.Values {
	q := url.Values{"range": {w.Key}}
	if w.zoomed() {
		q.Set("from", strconv.FormatInt(w.To.Add(-w.Span).UnixMilli(), 10))
		q.Set("to", strconv.FormatInt(w.To.UnixMilli(), 10))
	}
	return q
}

// overviewRange は ?range= を読む。知らない値なら既定の期間
func overviewRange(r *http.Request) (string, time.Duration) {
	key := r.URL.Query().Get("range")
	for _, rg := range overviewRanges {
		if rg.Key == key {
			return rg.Key, rg.Span
		}
	}
	for _, rg := range overviewRanges {
		if rg.Key == overviewDefaultRange {
			return rg.Key, rg.Span
		}
	}
	return overviewRanges[0].Key, overviewRanges[0].Span
}

func overviewStreamURLFor(w overviewWindow) string {
	return overviewStreamURL + "?" + w.query().Encode()
}

// GetOverviewStreamHandler は選んだ期間の推移と今の様子を、refresh ごとに SSE で送る
type GetOverviewStreamHandler struct {
	getRuntimeSnapshotUsecase *usecase.GetRuntimeSnapshotUsecase
	listIssuesUsecase         *usecase.ListIssuesUsecase
	getMetricsUsecase         *usecase.GetMetricsUsecase
	interval                  time.Duration
	// thresholds は Issue の閾値。推移のグラフに線を引く
	thresholds domain.IssuePolicy
}

func NewGetOverviewStreamHandler(getRuntimeSnapshotUsecase *usecase.GetRuntimeSnapshotUsecase, listIssuesUsecase *usecase.ListIssuesUsecase, getMetricsUsecase *usecase.GetMetricsUsecase, interval time.Duration, thresholds domain.IssuePolicy) *GetOverviewStreamHandler {
	return &GetOverviewStreamHandler{
		getRuntimeSnapshotUsecase: getRuntimeSnapshotUsecase,
		listIssuesUsecase:         listIssuesUsecase,
		getMetricsUsecase:         getMetricsUsecase,
		interval:                  interval,
		thresholds:                thresholds,
	}
}

func (h *GetOverviewStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	window := overviewWindowOf(r)
	// 画面を開いている間ずっと書き続けるので、http.Server の WriteTimeout をこのリクエストだけ外す
	if err := http.NewResponseController(rw).SetWriteDeadline(time.Time{}); err != nil {
		slog.ErrorContext(ctx, "clear write deadline for overview stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		slog.ErrorContext(ctx, "open overview stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		now := time.Now()
		end, span := window.bounds(now)
		metrics, err := h.getMetricsUsecase.Exec(ctx, usecase.GetMetricsUsecaseInput{Now: end, Span: span})
		if err != nil {
			slog.WarnContext(ctx, "get metrics", slog.Any("err", err))
		} else {
			vm := toOverviewViewModel(now, span, metrics, h.thresholds)
			if window.zoomed() {
				vm.ResetURL = "/?" + url.Values{"range": {window.Key}}.Encode()
			}
			if out, err := h.getRuntimeSnapshotUsecase.Exec(ctx, usecase.GetRuntimeSnapshotUsecaseInput{}); err == nil {
				addSnapshot(&vm, out, metrics)
			}
			if issues, err := h.listIssuesUsecase.Exec(ctx, usecase.ListIssuesUsecaseInput{Now: now}); err == nil {
				addIssues(&vm, issues.Issues, now)
			}
			if err := sse.Write(ctx, partials.OverviewStreamUpdate(vm)); err != nil {
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func toOverviewViewModel(now time.Time, span time.Duration, m usecase.GetMetricsUsecaseOutput, thresholds domain.IssuePolicy) components.OverviewViewModel {
	f := newChartFrame(m, span)
	vm := components.OverviewViewModel{
		At:    now.Format(time.TimeOnly),
		Range: fmt.Sprintf("%s – %s", f.label(m.From), f.label(m.To)),
		Step:  formatStep(m.Step),
		Steps: f.stepsJSON(),
		LogsURL: "/logs?" + url.Values{
			"from": {strconv.FormatInt(m.From.UnixNano(), 10)},
			"to":   {strconv.FormatInt(m.To.UnixNano(), 10)},
		}.Encode(),
	}
	if m.HasSince && m.Since.After(m.From) {
		vm.DataSince = f.label(m.Since)
	}

	states := []struct {
		label string
		cat   model.GoroutineStateCategory
	}{
		// いちばん多い waiting を下に積み、増え方 (リーク) を底から見えるようにする
		{"waiting", model.GoroutineWaiting},
		{"syscall", model.GoroutineSyscall},
		{"runnable", model.GoroutineRunnable},
		{"running", model.GoroutineRunning},
	}
	var goroutines []chartSeries
	for _, s := range states {
		cs := chartSeries{label: s.label, class: "state-" + s.label}
		for _, step := range m.Steps {
			cs.values = append(cs.values, step.States[s.cat])
		}
		goroutines = append(goroutines, cs)
	}
	vm.Goroutines = f.stackedBarChart(goroutines)
	vm.Goroutines.Notes = originNotesJSON(len(m.Steps), m.Origins)

	small := func(v float64) string { return strconv.FormatFloat(v, 'g', 2, 64) }
	cpuWaiting := pick(m, func(s usecase.MetricStepSummary) usecase.MetricSample { return s.CPUWaiting })
	vm.CPUWaiting = f.lineChart([]chartSeries{
		{label: "average", class: overviewSeriesClass + "1", values: cpuWaiting},
		{label: "max", class: overviewSeriesClass + "2", values: maxOf(cpuWaiting)},
	}, small, thresholds.CPUWaitRatio, 1)
	vm.GC = f.lineChart([]chartSeries{{label: "GC / s", class: overviewSeriesClass + "1", values: pick(m, func(s usecase.MetricStepSummary) usecase.MetricSample { return s.GCPerSecond })}}, small, thresholds.GCPerSecond, 1)
	vm.Heap = f.lineChart([]chartSeries{
		{label: "heap goal", class: overviewSeriesClass + "1", values: pick(m, func(s usecase.MetricStepSummary) usecase.MetricSample { return s.HeapGoal })},
		{label: "live heap", class: overviewSeriesClass + "2", values: pick(m, func(s usecase.MetricStepSummary) usecase.MetricSample { return s.LiveHeap })},
		{label: "heap objects", class: overviewSeriesClass + "3", values: pick(m, func(s usecase.MetricStepSummary) usecase.MetricSample { return s.HeapObjects })},
	}, func(v float64) string { return formatBytes(uint64(v)) }, 0, 1<<20)
	if gogc, ok := lastSample(pick(m, func(s usecase.MetricStepSummary) usecase.MetricSample { return s.EffectiveGOGC })); ok {
		vm.EffectiveGOGC = strconv.Itoa(int(gogc + 0.5))
	}
	vm.MarkAssist = f.lineChart([]chartSeries{
		{label: "mark assist", class: overviewSeriesClass + "1", values: scale(pick(m, func(s usecase.MetricStepSummary) usecase.MetricSample { return s.MarkAssist }), 1000)},
	}, small, 0, 1)
	return vm
}

func lastSample(samples []usecase.MetricSample) (float64, bool) {
	for i := len(samples) - 1; i >= 0; i-- {
		if samples[i].OK {
			return samples[i].Value, true
		}
	}
	return 0, false
}

func scale(samples []usecase.MetricSample, k float64) []usecase.MetricSample {
	out := make([]usecase.MetricSample, len(samples))
	for i, s := range samples {
		out[i] = usecase.MetricSample{Value: s.Value * k, Max: s.Max * k, OK: s.OK}
	}
	return out
}

// maxOf は区切りの中の最大を系列にする。平均では薄まる、短い間の跳ね上がりを見せる
func maxOf(samples []usecase.MetricSample) []usecase.MetricSample {
	out := make([]usecase.MetricSample, len(samples))
	for i, s := range samples {
		out[i] = usecase.MetricSample{Value: s.Max, OK: s.OK}
	}
	return out
}

func pick(m usecase.GetMetricsUsecaseOutput, f func(usecase.MetricStepSummary) usecase.MetricSample) []usecase.MetricSample {
	out := make([]usecase.MetricSample, 0, len(m.Steps))
	for _, s := range m.Steps {
		out = append(out, f(s))
	}
	return out
}

// addSnapshot は今の G の数・状態・生成元と、再生できる区間を足す。生成元には期間中の最大を添える
func addSnapshot(vm *components.OverviewViewModel, out usecase.GetRuntimeSnapshotUsecaseOutput, m usecase.GetMetricsUsecaseOutput) {
	s := out.Snapshot
	vm.HasSnapshot = true
	vm.Procs = len(out.Lanes)
	vm.GoroutinesNow = s.GoroutineCount
	vm.DumpAt = s.DumpAt.Format(time.TimeOnly)
	vm.LeakCheck = string(s.LeakCheckStatus)
	for _, st := range []struct {
		label string
		cat   model.GoroutineStateCategory
	}{
		{"running", model.GoroutineRunning}, {"runnable", model.GoroutineRunnable}, {"syscall", model.GoroutineSyscall}, {"waiting", model.GoroutineWaiting},
	} {
		vm.States = append(vm.States, components.GoroutineStateViewModel{Label: st.label, Count: s.StateCounts[st.cat], Class: "state-" + st.label})
	}
	if s.HasRecording {
		vm.Replayable = fmt.Sprintf("%s – %s", s.Recording.From.Format(time.TimeOnly), s.Recording.To.Format(time.TimeOnly))
	}
	vm.Origins = originRows(s.TopOrigins, m.Origins)
}

// originRows は、最新の dump で多い生成元と、期間中に多かった生成元を合わせ、期間中の最大の多い順に並べる。
// 今は少なくても、期間の途中で膨らんだ生成元を見逃さないようにする
func originRows(now []usecase.RuntimeSnapshotOriginCount, inRange map[model.GoroutineOrigin][]usecase.MetricSample) []components.OriginRowViewModel {
	rows := map[model.GoroutineOrigin]*components.OriginRowViewModel{}
	row := func(o model.GoroutineOrigin) *components.OriginRowViewModel {
		r, ok := rows[o]
		if !ok {
			r = &components.OriginRowViewModel{Origin: string(o)}
			rows[o] = r
		}
		return r
	}
	for _, o := range now {
		r := row(o.Origin)
		r.Now, r.HasNow = o.Count, true
	}
	for o, samples := range inRange {
		for _, smp := range samples {
			if smp.OK {
				r := row(o)
				r.Max, r.HasMax = max(r.Max, int(smp.Max+0.5)), true
			}
		}
	}
	out := make([]components.OriginRowViewModel, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b components.OriginRowViewModel) int {
		return cmp.Or(cmp.Compare(b.Max, a.Max), cmp.Compare(b.Now, a.Now), cmp.Compare(a.Origin, b.Origin))
	})
	return out[:min(overviewOriginsLimit, len(out))]
}

// originNotesJSON は区切りごとに、G の多かった生成元を上位 overviewOriginNotes 個まで並べる。Goroutines の hover で出す
func originNotesJSON(steps int, inRange map[model.GoroutineOrigin][]usecase.MetricSample) string {
	type note struct {
		Label string `json:"label"`
		Text  string `json:"text"`
	}
	type count struct {
		origin model.GoroutineOrigin
		value  float64
	}
	notes := make([][]note, steps)
	for i := range steps {
		var counts []count
		for o, samples := range inRange {
			if i < len(samples) && samples[i].OK {
				counts = append(counts, count{o, samples[i].Value})
			}
		}
		slices.SortFunc(counts, func(a, b count) int { return cmp.Or(cmp.Compare(b.value, a.value), cmp.Compare(a.origin, b.origin)) })
		for _, c := range counts[:min(overviewOriginNotes, len(counts))] {
			notes[i] = append(notes[i], note{Label: components.ShortPath(string(c.origin)), Text: formatCount(math.Round(c.value*10) / 10)})
		}
	}
	b, err := json.Marshal(notes)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func addIssues(vm *components.OverviewViewModel, issues []domain.Issue, now time.Time) {
	vm.IssueCount = len(issues)
	for _, i := range issues[:min(overviewIssueRows, len(issues))] {
		row := components.OverviewIssueViewModel{Kind: string(i.Kind), Certainty: string(i.Certainty), Subject: i.Subject, Summary: i.Summary, Href: "/issues"}
		if !i.At.IsZero() {
			row.Age = formatAge(now.Sub(i.At))
		}
		if links := toIssueViewModel(i).Links; len(links) > 0 {
			row.Href = links[0].URL
		}
		vm.Issues = append(vm.Issues, row)
	}
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}
