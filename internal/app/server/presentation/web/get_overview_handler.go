package web

import (
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// GetOverviewHandler はセッション全体の様子を描く。期間が動く (セッション全体・直近) ときは、SSE で中身を描き直す
type GetOverviewHandler struct {
	status   *usecase.GetSessionStatusUsecase
	overview *usecase.GetOverviewUsecase
}

func NewGetOverviewHandler(status *usecase.GetSessionStatusUsecase, overview *usecase.GetOverviewUsecase) *GetOverviewHandler {
	return &GetOverviewHandler{status: status, overview: overview}
}

func (h *GetOverviewHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
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
	out, err := h.overview.Exec(r.Context(), usecase.GetOverviewUsecaseInput{From: tr.From, To: tr.To, HasRange: true})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	vm := pages.OverviewViewModel{Base: baseOf("Overview", tabOverview, st), Picker: picker("/overview", nil, tr, phasesOf(st.Timeline, st.Elapsed)), Body: overviewBody(st, out)}
	if tr.Live() && st.Session.App.State == domain.AppRunning {
		vm.StreamURL = "/overview/stream?" + tr.query().Encode()
	}
	render(rw, r, pages.Overview(vm))
}

func overviewBody(st usecase.GetSessionStatusUsecaseOutput, out usecase.GetOverviewUsecaseOutput) partials.OverviewBodyViewModel {
	app := out.Session.App
	vm := partials.OverviewBodyViewModel{Error: strings.TrimSpace(app.BuildError + app.StartError), IssuesURL: "/issues"}
	for _, o := range out.Ongoing {
		vm.Ongoing = append(vm.Ongoing, partials.OngoingIssueViewModel{Kind: string(o.Kind), Subject: o.Target, Certainty: string(o.Certainty), Phase: o.Phase, Last: at(o.LastAt), URL: issueURL(o.ID, 0)})
	}
	vm.Goroutines, vm.Threads = "-", "-"
	if n := len(out.Buckets); n > 0 {
		last := out.Buckets[n-1]
		vm.Goroutines, vm.GoroutinesAt, vm.Threads = itoa(last.Goroutines), " at "+at(out.To), itoa(last.Threads)
	}
	for _, k := range domain.IssueKinds {
		vm.Kinds = append(vm.Kinds, string(k))
	}
	for _, p := range out.Phases {
		row := partials.OverviewPhaseViewModel{Name: p.Phase, Range: span(p.From, p.To), URL: "/issues?" + timeRange{FromRaw: at(p.From), ToRaw: at(p.To)}.query().Encode()}
		for _, k := range domain.IssueKinds {
			c := "·"
			if n := p.Issues[k]; n > 0 {
				c = itoa(n)
			}
			row.Counts = append(row.Counts, c)
		}
		vm.Phases = append(vm.Phases, row)
	}
	vm.Charts = overviewCharts(out, st.Timeline)
	return vm
}

// sample は時間幅をグラフの区切りの長さにまとめたもの
type sample struct {
	n                                    int
	goroutines, runnable, threads        float64
	heap, goal                           float64
	wait                                 domain.Histogram
	running, lock, gcWorker, assist, stw time.Duration
}

// samplesOf は 1 秒ごとの時間幅を、区切りが多くなりすぎないよう step ごとにまとめる。値の数・ヒープ・スレッドは区切りの終わりの値
func samplesOf(bs []domain.Bucket, first time.Duration, step time.Duration) ([]time.Duration, []sample) {
	per := max(1, int(step/domain.BucketSize))
	var times []time.Duration
	var out []sample
	for i := 0; i < len(bs); i += per {
		var s sample
		for _, b := range bs[i:min(len(bs), i+per)] {
			s.n++
			s.goroutines, s.threads, s.heap, s.goal = float64(b.Goroutines), float64(b.Threads), float64(b.HeapObjects), float64(b.HeapGoal)
			s.runnable += float64(b.Runnable)
			s.wait.Merge(&b.RunnableWait)
			s.running += b.Running
			s.lock += b.LockWait
			s.gcWorker += b.GCWorker
			s.assist += b.MarkAssist
			s.stw += b.STW
		}
		s.runnable /= float64(s.n)
		times = append(times, first+time.Duration(i)*domain.BucketSize)
		out = append(out, s)
	}
	return times, out
}

const maxChartPoints = 120

func overviewCharts(out usecase.GetOverviewUsecaseOutput, events []domain.SessionEvent) partials.OverviewChartsViewModel {
	span := max(out.To-out.From, time.Second)
	step := max(domain.BucketSize, (span/maxChartPoints+domain.BucketSize-1)/domain.BucketSize*domain.BucketSize)
	first := out.From / domain.BucketSize * domain.BucketSize
	times, samples := samplesOf(out.Buckets, first, step)
	procs := time.Duration(max(out.Procs, 1))
	capacity := func(s sample) float64 { return float64(procs * time.Duration(s.n) * domain.BucketSize) }
	vals := func(f func(s sample) (float64, bool)) []float64 {
		v := make([]float64, len(samples))
		for i, s := range samples {
			if x, ok := f(s); ok {
				v[i] = x
			} else {
				v[i] = math.NaN()
			}
		}
		return v
	}
	var marks []chartMark
	for _, e := range events {
		if e.Kind != domain.EventAppStart {
			marks = append(marks, chartMark{at: e.At, class: "start", title: eventText(e)})
		}
	}
	spec := func(title string, format func(float64) string, ss ...series) chartSpec {
		return chartSpec{title: title, from: out.From, to: max(out.To, out.From+time.Second), step: step, times: times, series: ss, format: format, threshold: math.NaN(), marks: marks, zoom: true,
			hoverURL: func(i int) string { return schedulerURL(times[i], times[i]+min(step, 2*time.Second)) }}
	}

	goroutines := spec("Goroutines", count, series{label: "goroutines", class: "series-1", values: vals(func(s sample) (float64, bool) { return s.goroutines, true })})

	cpu := spec("CPU wait: RUNNABLE → RUNNING p99", func(v float64) string { return duration(time.Duration(v)) },
		series{label: "p99", class: "series-2", values: vals(func(s sample) (float64, bool) { return float64(s.wait.Quantile(0.99)), s.wait.Total > 0 })})
	cpu.threshold, cpu.thLabel = float64(time.Millisecond), "CPU_CONTENTION: p99 ≥ 1ms"

	runnable := spec("Waiting for P", func(v float64) string { return trimFloat(v, 2) },
		series{label: "goroutines waiting", class: "series-2", values: vals(func(s sample) (float64, bool) { return s.runnable, true })})

	heap := spec("Heap", bytesOf,
		series{label: "heap", class: "series-3", values: vals(func(s sample) (float64, bool) { return s.heap, s.heap > 0 })},
		series{label: "goal", class: "series-3", dashed: true, values: vals(func(s sample) (float64, bool) { return s.goal, s.goal > 0 })})

	gc := spec("GC", percent, series{label: "GC", class: "series-4", values: vals(func(s sample) (float64, bool) {
		return float64(s.gcWorker+s.assist+s.stw*procs) / capacity(s), true
	})})
	gc.threshold, gc.thLabel = 0.5, "GC_PRESSURE (FACT): GC ≥ 50%"
	assist := spec("Mark assist", percent, series{label: "mark assist", class: "series-5", values: vals(func(s sample) (float64, bool) { return float64(s.assist) / capacity(s), true })})
	assist.threshold, assist.thLabel = 0.05, "GC_PRESSURE: mark assist > 5%"

	lock := spec("Lock wait per running time", percent,
		series{label: "lock wait / running", class: "series-6", values: vals(func(s sample) (float64, bool) { return float64(s.lock) / float64(s.running), s.running > 0 })})
	lock.threshold, lock.thLabel = 1, "LOCK_CONTENTION: lock wait > running"

	threads := spec("OS threads", count, series{label: "threads", class: "series-1", values: vals(func(s sample) (float64, bool) { return s.threads, s.threads > 0 })})
	threads.threshold, threads.thLabel = 5000, "THREAD_EXPLOSION: more than half of the 10,000 limit"

	return partials.OverviewChartsViewModel{Goroutines: buildChart(goroutines), Heap: buildChart(heap), GC: buildChart(gc), MarkAssist: buildChart(assist),
		WaitToRun: buildChart(cpu), Runnable: buildChart(runnable), LockWait: buildChart(lock), Threads: buildChart(threads)}
}
