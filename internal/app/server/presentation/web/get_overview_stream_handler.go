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

const overviewPoll = 2 * time.Second

// GetOverviewStreamHandler は期間が動く Overview の中身を、SSE の turbo-stream で描き直す
type GetOverviewStreamHandler struct {
	status   *usecase.GetSessionStatusUsecase
	overview *usecase.GetOverviewUsecase
}

func NewGetOverviewStreamHandler(status *usecase.GetSessionStatusUsecase, overview *usecase.GetOverviewUsecase) *GetOverviewStreamHandler {
	return &GetOverviewStreamHandler{status: status, overview: overview}
}

func (h *GetOverviewStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		middleware.Fail(r, domain.ErrInternal.Wrap(err, "open the overview stream"))
		return
	}
	q := r.URL.Query()
	streamEvery(r.Context(), overviewPoll, func(ctx context.Context) error {
		st, err := h.status.Exec(ctx, usecase.GetSessionStatusUsecaseInput{})
		if err != nil {
			return err
		}
		tr, err := parseRange(q, st.Elapsed, fromSession)
		if err != nil {
			return err
		}
		out, err := h.overview.Exec(ctx, usecase.GetOverviewUsecaseInput{From: tr.From, To: tr.To, HasRange: true})
		if err != nil {
			return err
		}
		return sse.Write(ctx, partials.OverviewStream(overviewBody(st, out)))
	})
}
