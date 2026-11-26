package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type goroutineDetailResponse struct {
	Goroutine goroutineResponse `json:"goroutine"`
	Next      []string          `json:"next"`
}

// GetGoroutineHandler は 1 つの goroutine の詳細を返す
type GetGoroutineHandler struct {
	detail *usecase.GetGoroutineDetailUsecase
	paths  paths
}

func NewGetGoroutineHandler(detail *usecase.GetGoroutineDetailUsecase, guiBase string) *GetGoroutineHandler {
	return &GetGoroutineHandler{detail: detail, paths: paths{base: guiBase}}
}

func rangeInput(r *http.Request) (from, to time.Duration, has bool, err error) {
	q := r.URL.Query()
	if q.Get("from") == "" && q.Get("to") == "" {
		return 0, 0, false, nil
	}
	f, err := parseAt(q.Get("from"))
	if err != nil {
		return 0, 0, false, err
	}
	t, err := parseAt(q.Get("to"))
	if err != nil {
		return 0, 0, false, err
	}
	return f, t, true, nil
}

func (h *GetGoroutineHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Fail(r, domain.ErrInvalidArgument.With(fmt.Sprintf("goroutine id %q is not a number", r.PathValue("id"))))
		return
	}
	from, to, has, err := rangeInput(r)
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	out, err := h.detail.Exec(r.Context(), usecase.GetGoroutineDetailUsecaseInput{ID: domain.GoroutineID(id), From: from, To: to, HasRange: has})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	resp := goroutineDetailResponse{Goroutine: goroutineOf(h.paths, out.Detail, true)}
	for _, w := range out.Detail.Wakers {
		resp.Next = append(resp.Next, goroutineCommand(w.G, w.WaitFrom, w.WaitTo))
	}
	resp.Next = append(resp.Next, "gorviz docs goroutines")
	if err := write(rw, r, resp, 0); err != nil {
		middleware.Fail(r, err)
	}
}
