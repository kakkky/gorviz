package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/layout"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// GetGoroutineHandler は Goroutine の画面。診断と、調べる期間の GMP の再生を出す
type GetGoroutineHandler struct {
	nav      navigator
	diagnose *usecase.DiagnoseGoroutineUsecase
	chunks   *usecase.ListTraceChunksUsecase
}

func NewGetGoroutineHandler(services *usecase.ListServicesUsecase, diagnose *usecase.DiagnoseGoroutineUsecase, chunks *usecase.ListTraceChunksUsecase) *GetGoroutineHandler {
	return &GetGoroutineHandler{nav: navigator{services: services}, diagnose: diagnose, chunks: chunks}
}

var periodByText = map[domain.PeriodBy]string{
	domain.PeriodGiven:      "the range given (the replayed range when opened from the Scheduler)",
	domain.PeriodLog:        "from the log line: the request it ended, or around the time it was written",
	domain.PeriodOccurrence: "around the issue occurrence",
	domain.PeriodWoken:      "while the goroutine it woke was waiting",
}

var sourceText = map[domain.DiagnosisSource]string{
	domain.SourceExecutionTrace:  "execution trace",
	domain.SourceStateBreakdowns: "per-minute time by state",
	domain.SourceSnapshots:       "goroutine snapshot",
}

func (h *GetGoroutineHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	p := pathInstance(r)
	g, err := parseGoroutine(r.PathValue("goroutine"), "goroutine")
	if err == nil && g == 0 {
		err = domain.ErrInvalidArgument.With("goroutine must be a goroutine id")
	}
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	q := r.URL.Query()
	pq, err := parsePeriodQuery(q, now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	tr, _ := parseRange(q, now)
	base, _, err := h.nav.base(r.Context(), "G"+itoa(int(g)), p.service, p.instance, "")
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	out, err := h.diagnose.Exec(r.Context(), usecase.DiagnoseGoroutineUsecaseInput{Service: p.service, Instance: p.instance, Goroutine: g, Period: pq, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	chunks, err := h.chunks.Exec(r.Context(), usecase.ListTraceChunksUsecaseInput{Service: p.service, Instance: p.instance, From: out.Period.From, To: out.Period.To, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	render(rw, r, pages.Goroutine(goroutineView(base, p, g, tr, out, chunks)))
}

func goroutineView(base layout.BaseViewModel, p instancePath, g observation.GoroutineID, tr timeRange, out usecase.DiagnoseGoroutineUsecaseOutput, chunks usecase.ListTraceChunksUsecaseOutput) pages.GoroutineViewModel {
	s, i, d := p.service, p.instance, out.Diagnosis
	self := paths.Goroutine(s, i, g, time.Time{}, time.Time{})
	vm := pages.GoroutineViewModel{Base: base, Goroutine: "G" + itoa(int(g)), Instance: string(i), Period: spanMilli(out.Period.From, out.Period.To), PeriodBy: periodByText[out.Period.By],
		Found: out.Found, Entry: d.Entry, CreatedBy: d.CreatedBy.Function, Main: string(d.Main), MainCertainty: string(d.MainCertainty), MainNote: mainCertaintyNote(d.MainCertainty),
		Source: sourceText[d.Source], SchedulerURL: paths.Scheduler(s, i, out.Period.From, out.Period.To), Picker: picker(self, nil, tr, out.Period.From, out.Period.To, nil, "")}
	vm.Picker.Live, vm.PeriodRange = false, rangeView(out.Period.From, out.Period.To)
	if d.CreatedBy.Function != "" {
		vm.CreatedByAt = d.CreatedBy.Location()
	}
	if vm.Entry == "" {
		vm.Entry = "not recorded"
	}
	if vm.Source == "" {
		vm.Source = "nothing"
	}
	switch {
	case d.MinuteAligned:
		vm.SourceNote = "the period is aligned to whole minutes (" + span(d.ComputedFrom.From, d.ComputedFrom.To) + ")"
	case out.TraceFromPreserved:
		vm.SourceNote = "the trace kept around an issue occurrence or an error log"
	case d.Source == domain.SourceSnapshots:
		vm.SourceNote = "the state at " + dateTime(d.ComputedFrom.To)
	}
	for _, u := range unavailableViews(out.Unavailable) {
		if u.Field != unavailableFields["schedulerReplay"] {
			vm.Unavailable = append(vm.Unavailable, u)
		}
	}
	if b := d.Breakdown; b != nil {
		bv := &pages.BreakdownViewModel{}
		total := b.Total()
		for _, c := range domain.MainCauses {
			if dur := b.Durations[c]; dur > 0 && total > 0 {
				share := float64(dur) / float64(total)
				bv.Rows = append(bv.Rows, pages.BreakdownRowViewModel{Cause: string(c), Width: strconv.FormatFloat(share*100, 'f', 1, 64) + "%", Percent: percent(share), Duration: duration(dur)})
			}
		}
		if b.Unrecorded > 0 {
			bv.Unrecorded = duration(b.Unrecorded)
		}
		vm.Breakdown = bv
	}
	if st := d.Stop; st != nil {
		vm.Stop = &pages.StopViewModel{Cause: string(st.Cause), Function: st.Function, Location: st.Location, WaitReason: st.WaitReason, Duration: duration(st.Duration)}
	}
	vm.Leak = leakView(out.Leak)
	const maxWakers = 8
	for n, w := range d.Wakers {
		if n == maxWakers {
			vm.MoreWakers = len(d.Wakers) - maxWakers
			break
		}
		vm.Wakers = append(vm.Wakers, pages.WakerViewModel{Goroutine: "G" + itoa(int(w.Goroutine)), Function: w.Function, Location: w.Location, Cause: string(w.Cause), Count: w.Count,
			Waited: duration(w.Waited), URL: goroutineWoken(s, i, w.Goroutine, g, w.From, w.To)})
	}
	for _, l := range out.Logs {
		lq := rangeQuery(l.At.Add(-5*time.Minute), l.At.Add(5*time.Minute))
		lq.Set("log", string(l.ID()))
		row := pages.GoroutineLogViewModel{At: clockMilli(l.At), Level: levelText(l.Level), LevelClass: levelClass(l.Level), Message: l.Message, URL: withQuery(servicePath(s, "logs"), lq) + "#log-" + logAnchor(l.ID())}
		if l.HasDuration {
			row.Duration = duration(l.Duration)
		}
		vm.Logs = append(vm.Logs, row)
	}
	props := components.GMPReplayProps{FocusGoroutine: int64(g), GoroutineURL: goroutineTemplate(s, i)}
	rv := replayView(p, chunks, props)
	vm.Replay, vm.ReplayFrom, vm.ReplayGone, vm.Nearby, vm.LeakNote = rv.replay, chunks.FromPreserved, rv.unavailable, rv.nearby, rv.leakNote
	return vm
}

func leakView(l domain.LeakStatus) pages.LeakViewModel {
	v := pages.LeakViewModel{Leaked: l.Leaked}
	if !l.CheckedAt.IsZero() {
		v.CheckedAt = dateTime(l.CheckedAt)
	}
	switch {
	case !l.Checked && l.Status == "":
		v.State = "No leak check is kept for this period (" + pastRetention + " or not taken yet)."
	case !l.Checked:
		v.State = "The runtime cannot check leaks here (" + string(l.Status) + ")."
	case l.Leaked:
		v.State = "同じスタックの " + itoa(l.Of) + " 個のうち " + itoa(l.Count) + " 個がリーク"
		v.Certainty, v.CertaintyNote = string(l.Certainty), leakCertaintyNote(l.Certainty)
	default:
		v.State = "Not judged as leaked by the runtime."
	}
	return v
}

func mainCertaintyNote(c domain.Certainty) string {
	switch c {
	case domain.CertaintyFact:
		return "FACT: trace のチャンクの状態の変化から決めた"
	case domain.CertaintyInference:
		return "INFERENCE: goroutine のスナップショットの、スタックから推定した待機理由だけで決めた"
	}
	return ""
}
