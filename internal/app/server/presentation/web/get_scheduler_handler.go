package web

import (
	"net/http"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const schedulerPoll = 2 * time.Second

// GetSchedulerHandler はスケジューラの動きを再生する。期間がなければ直近を追い、新しく届いた分を SSE で足す
type GetSchedulerHandler struct {
	status *usecase.GetSessionStatusUsecase
	window *usecase.GetSchedulerWindowUsecase
}

func NewGetSchedulerHandler(status *usecase.GetSessionStatusUsecase, window *usecase.GetSchedulerWindowUsecase) *GetSchedulerHandler {
	return &GetSchedulerHandler{status: status, window: window}
}

func (h *GetSchedulerHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	st, err := h.status.Exec(r.Context(), usecase.GetSessionStatusUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	in := usecase.GetSchedulerWindowUsecaseInput{}
	var tr timeRange
	if r.URL.Query().Get("from") != "" {
		if tr, err = parseRange(r.URL.Query(), st.Elapsed, fromSession); err != nil {
			middleware.Fail(r, err)
			return
		}
		in.From, in.To, in.HasRange = tr.From, tr.To, true
	}
	out, err := h.window.Exec(r.Context(), in)
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	w := out.Window
	if !in.HasRange {
		tr = timeRange{FromRaw: "-" + usecase.LiveWindow.String(), From: w.From, To: w.To}
	}
	live := w.Live && !w.Ended
	vm := pages.SchedulerViewModel{Base: baseOf("Scheduler", tabScheduler, st), Picker: picker("/scheduler", nil, tr, phasesOf(st.Timeline, st.Elapsed)), Past: in.HasRange, LatestURL: "/scheduler", Truncated: w.Truncated}
	props := components.GMPReplayProps{Live: live, GoroutineURL: "/goroutines/{goroutine}"}
	if in.HasRange {
		d := w.To - w.From
		if w.From > 0 {
			props.PrevURL = schedulerURL(max(0, w.From-d), w.From)
		}
		if w.To < st.Elapsed {
			props.NextURL = schedulerURL(w.To, min(st.Elapsed, w.To+d))
		}
	}
	vm.Replay = components.GMPReplayViewModel{Props: propsJSON(props), Window: replayWindow(w, out.Leaks), FeedID: partials.ReplayFeed}
	if live {
		vm.StreamURL = "/scheduler/stream?after=" + at(w.To)
	}
	render(rw, r, pages.Scheduler(vm))
}
