package web

import (
	"context"
	"net/http"
	"time"

	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// GetSchedulerStreamHandler は最新を追っている再生に、新しく届いた分の窓を SSE の turbo-stream で足していく
type GetSchedulerStreamHandler struct {
	window *usecase.GetSchedulerWindowUsecase
}

func NewGetSchedulerStreamHandler(window *usecase.GetSchedulerWindowUsecase) *GetSchedulerStreamHandler {
	return &GetSchedulerStreamHandler{window: window}
}

func (h *GetSchedulerStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	cursor, err := time.ParseDuration(r.URL.Query().Get("after"))
	if err != nil {
		middleware.Fail(r, domain.ErrInvalidArgument.With("after must be the end of the replayed window, like 12.5s"))
		return
	}
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		middleware.Fail(r, domain.ErrInternal.Wrap(err, "open the scheduler stream"))
		return
	}
	streamEvery(r.Context(), schedulerPoll, func(ctx context.Context) error {
		out, err := h.window.Exec(ctx, usecase.GetSchedulerWindowUsecaseInput{From: cursor, To: cursor + usecase.LiveWindow, HasRange: true})
		if err != nil || out.Window.To <= cursor {
			return sse.Heartbeat()
		}
		cursor = out.Window.To
		return sse.Write(ctx, partials.ReplayWindowStream(replayWindow(out.Window, out.Leaks)))
	})
}
