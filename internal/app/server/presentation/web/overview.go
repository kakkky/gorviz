package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// GetRootHandler は Overview へ移す
type GetRootHandler struct{}

func NewGetRootHandler() *GetRootHandler { return &GetRootHandler{} }

func (GetRootHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	http.Redirect(rw, r, "/overview", http.StatusSeeOther)
}

// GetOverviewHandler はセッション全体の様子を描く
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
	out, err := h.overview.Exec(r.Context(), usecase.GetOverviewUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	render(rw, r, pages.Overview(overviewViewModel(st, out)))
}

func overviewViewModel(st usecase.GetSessionStatusUsecaseOutput, out usecase.GetOverviewUsecaseOutput) pages.OverviewViewModel {
	app := out.Session.App
	vm := pages.OverviewViewModel{Base: baseOf("Overview", tabOverview, st, focus{})}
	if msg := strings.TrimSpace(app.BuildError + app.StartError); msg != "" {
		vm.Message = msg
	}
	total := 0
	for _, n := range out.Issues {
		total += n
	}
	exit := "running"
	if app.Exited {
		exit = "exit " + strconv.Itoa(app.ExitCode)
	}
	vm.Stats = []pages.StatViewModel{
		{Label: "issues", Value: strconv.Itoa(total), URL: "/issues"},
		{Label: "app", Value: exit},
		{Label: "Ps (GOMAXPROCS)", Value: strconv.Itoa(out.Procs)},
		{Label: "Go", Value: app.GoVersion},
		{Label: "diagnoses", Value: strconv.Itoa(len(out.Session.Diagnoses))},
	}
	for _, k := range domain.IssueKinds {
		vm.Kinds = append(vm.Kinds, string(k))
	}
	for _, p := range out.Phases {
		row := pages.PhaseRowViewModel{Name: p.Phase, Range: span(p.From, p.To), URL: schedulerURL(p.From, min(p.To, p.From+2*time.Second))}
		for _, k := range domain.IssueKinds {
			c := "·"
			if n := p.Issues[k]; n > 0 {
				c = strconv.Itoa(n)
			}
			row.Counts = append(row.Counts, c)
		}
		vm.Phases = append(vm.Phases, row)
	}
	procs := time.Duration(max(out.Procs, 1))
	series := []struct {
		title, unit string
		value       func(b domain.Bucket) float64
		format      func(float64) string
	}{
		{"Goroutines of the app", "", func(b domain.Bucket) float64 { return float64(b.Goroutines) }, count},
		{"RUNNABLE goroutines", "", func(b domain.Bucket) float64 { return float64(b.Runnable) }, count},
		{"Wait for a P, p99", "ms", func(b domain.Bucket) float64 { return float64(b.RunnableWait.Quantile(0.99)) / 1e6 }, decimal},
		{"GC CPU share", "%", func(b domain.Bucket) float64 {
			return float64(b.GCWorker+b.MarkAssist+b.STW*procs) * 100 / float64(procs*domain.BucketSize)
		}, decimal},
		{"Lock wait", "s per s", func(b domain.Bucket) float64 { return b.LockWait.Seconds() }, decimal},
		{"Heap objects", "MB", func(b domain.Bucket) float64 { return float64(b.HeapObjects) / (1 << 20) }, decimal},
		{"OS threads", "", func(b domain.Bucket) float64 { return float64(b.Threads) }, count},
	}
	for _, s := range series {
		vm.Charts = append(vm.Charts, chartOf(s.title, s.unit, out.Buckets, s.value, s.format, st.Timeline, out.Elapsed))
	}
	return vm
}

func count(v float64) string { return strconv.Itoa(int(v)) }

func decimal(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

func chartOf(title, unit string, bs []domain.Bucket, value func(domain.Bucket) float64, format func(float64) string, events []domain.SessionEvent, elapsed time.Duration) components.ChartViewModel {
	vm := components.ChartViewModel{Title: title, Unit: unit}
	peak := 0.0
	vals := make([]float64, len(bs))
	for i, b := range bs {
		vals[i] = value(b)
		peak = max(peak, vals[i])
	}
	var pts []string
	for i, v := range vals {
		x := 0.0
		if len(vals) > 1 {
			x = float64(i) * 1000 / float64(len(vals)-1)
		}
		y := 100.0
		if peak > 0 {
			y = 100 - v*96/peak
		}
		pts = append(pts, fmt.Sprintf("%.1f,%.1f", x, y))
	}
	vm.Points = strings.Join(pts, " ")
	vm.Max = format(peak)
	if len(vals) > 0 {
		vm.Last = format(vals[len(vals)-1])
	}
	total := max(elapsed, time.Millisecond)
	for _, e := range events {
		if e.Kind == domain.EventPhase || e.Kind == domain.EventShutdown {
			name := e.Phase
			if name == "" {
				name = string(e.Kind)
			}
			vm.Marks = append(vm.Marks, components.ChartMarkViewModel{X: fmt.Sprintf("%.1f", float64(e.At)*1000/float64(total)), Title: name + " at " + at(e.At)})
		}
	}
	return vm
}
