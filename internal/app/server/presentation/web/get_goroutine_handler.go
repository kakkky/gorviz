package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const maxWakers = 5

// GetGoroutineHandler は 1 つの goroutine の詳細と、その期間のスケジューラの動きを描く
type GetGoroutineHandler struct {
	status *usecase.GetSessionStatusUsecase
	detail *usecase.GetGoroutineDetailUsecase
	window *usecase.GetSchedulerWindowUsecase
}

func NewGetGoroutineHandler(status *usecase.GetSessionStatusUsecase, detail *usecase.GetGoroutineDetailUsecase, window *usecase.GetSchedulerWindowUsecase) *GetGoroutineHandler {
	return &GetGoroutineHandler{status: status, detail: detail, window: window}
}

func (h *GetGoroutineHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Fail(r, domain.ErrInvalidArgument.With("the goroutine id is not a number"))
		return
	}
	st, err := h.status.Exec(r.Context(), usecase.GetSessionStatusUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	in := usecase.GetGoroutineDetailUsecaseInput{ID: domain.GoroutineID(id)}
	var tr timeRange
	if r.URL.Query().Get("from") != "" {
		if tr, err = parseRange(r.URL.Query(), st.Elapsed, fromSession); err != nil {
			middleware.Fail(r, err)
			return
		}
		in.From, in.To, in.HasRange = tr.From, tr.To, true
	}
	out, err := h.detail.Exec(r.Context(), in)
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	d := out.Detail
	if !in.HasRange {
		tr = timeRange{FromRaw: at(d.From), ToRaw: at(d.To), From: d.From, To: d.To}
	}
	win, err := h.window.Exec(r.Context(), usecase.GetSchedulerWindowUsecaseInput{From: d.From, To: max(d.To, d.From+1), HasRange: true})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	vm := goroutineViewModel(st, d, in.HasRange)
	vm.Picker = picker(fmt.Sprintf("/goroutines/%d", id), nil, tr, phasesOf(st.Timeline, st.Elapsed))
	props := components.GMPReplayProps{FocusGoroutine: id, GoroutineURL: "/goroutines/{goroutine}"}
	vm.Replay = components.GMPReplayViewModel{Props: propsJSON(props), Window: replayWindow(win.Window, win.Leaks), FeedID: partials.ReplayFeed}
	render(rw, r, pages.Goroutine(vm))
}

func goroutineViewModel(st usecase.GetSessionStatusUsecaseOutput, d domain.GoroutineDetail, opened bool) pages.GoroutineViewModel {
	vm := pages.GoroutineViewModel{Base: baseOf(gid(d.ID), "", st), Goroutine: gid(d.ID), Agent: d.Agent, Period: span(d.From, d.To), Found: d.HasObservations,
		Entry: d.Entry, CreatedBy: d.Creator.Function, CreatedByAt: d.Creator.Location(), Main: string(d.MainCause), MainCertainty: string(d.Certainty), MainNote: certaintyNote(d.Certainty),
		Truncated: d.TraceTruncated, SchedulerURL: schedulerURL(d.From, max(d.To, d.From+1)), PeriodRange: rangeView(d.From, d.To)}
	vm.PeriodBy = "the life of the goroutine, up to the last minute"
	if opened {
		vm.PeriodBy = "the range it was opened over"
	}
	if vm.CreatedBy == "" {
		vm.CreatedBy = "before the agent started"
	}
	if len(d.Breakdown) > 0 {
		var total, top int64
		for _, b := range d.Breakdown {
			total += int64(b.Duration)
			top = max(top, int64(b.Duration))
		}
		vm.Breakdown = &pages.BreakdownViewModel{}
		for _, b := range d.Breakdown {
			vm.Breakdown.Rows = append(vm.Breakdown.Rows, pages.BreakdownRowViewModel{Cause: string(b.Cause), Width: fmt.Sprintf("%.1f%%", float64(b.Duration)*100/float64(top)),
				Percent: fmt.Sprintf("%.1f%%", float64(b.Duration)*100/float64(total)), Duration: duration(b.Duration)})
		}
		if rest := int64(d.To-d.From) - total; rest > int64(d.To-d.From)/100 {
			vm.Breakdown.Unrecorded = duration(d.To - d.From - time.Duration(total))
		}
	}
	if d.StoppedAt.Function != "" {
		fn := d.StoppedAt.Function
		if d.StoppedIn != "" {
			fn += " in " + d.StoppedIn
		}
		vm.Stop = &pages.StopViewModel{Cause: string(d.MainCause), Function: fn, Location: d.StoppedAt.Location(), WaitReason: orDash(d.StoppedReason), Duration: duration(d.StoppedFor)}
	}
	vm.Leak = pages.LeakViewModel{State: "not judged leaked"}
	if d.Leak != nil {
		vm.Leak = pages.LeakViewModel{Leaked: true, State: fmt.Sprintf("%d of the %d goroutines waiting at the same place leaked", d.Leak.Count, d.Leak.Of), Certainty: string(d.Leak.Certainty), CertaintyNote: certaintyNote(d.Leak.Certainty)}
	}
	for i, w := range d.Wakers {
		if i == maxWakers {
			vm.MoreWakers = len(d.Wakers) - maxWakers
			break
		}
		vm.Wakers = append(vm.Wakers, pages.WakerViewModel{Goroutine: gid(w.G), Function: w.Function, Location: w.Location, Cause: string(w.Cause), Count: w.Count, Waited: duration(w.Total), URL: goroutineURL(w.G, w.WaitFrom, w.WaitTo)})
	}
	for _, f := range d.Stack {
		vm.Stack = append(vm.Stack, pages.StackFrameViewModel{Function: f.Function, Location: f.Location()})
	}
	return vm
}
