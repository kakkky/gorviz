package web

import (
	"net/http"
	"strconv"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

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
	from, to, has, err := rangeOf(r)
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	st, err := h.status.Exec(r.Context(), usecase.GetSessionStatusUsecaseInput{})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	out, err := h.detail.Exec(r.Context(), usecase.GetGoroutineDetailUsecaseInput{ID: domain.GoroutineID(id), From: from, To: to, HasRange: has})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	d := out.Detail
	win, err := h.window.Exec(r.Context(), usecase.GetSchedulerWindowUsecaseInput{From: d.From, To: max(d.To, d.From+1), HasRange: true})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	props := components.GMPReplayProps{FocusGoroutine: id, GoroutineURL: "/goroutines/{goroutine}"}
	vm := pages.GoroutineViewModel{Base: baseOf("G"+strconv.FormatInt(id, 10), "", st, focus{from: d.From, to: d.To, has: true}), Card: cardOf(d),
		Replay: components.GMPReplayViewModel{Props: propsJSON(props), Window: replayWindow(win.Window, win.Leaks), FeedID: partials.ReplayFeed}}
	render(rw, r, pages.Goroutine(vm))
}
