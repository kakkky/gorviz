package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// GetLogOpenHandler はログ 1 行を書いた瞬間を開く。書いた G が分かればその G の Goroutine 画面 (所要時間があればそのリクエストの区間)、
// 分からなければその時刻の Scheduler に移す。どちらも再生にこのログの印を付ける
type GetLogOpenHandler struct {
	locateLogUsecase *usecase.LocateLogUsecase
	defaultLength    time.Duration
}

func NewGetLogOpenHandler(locateLogUsecase *usecase.LocateLogUsecase, defaultLength time.Duration) *GetLogOpenHandler {
	return &GetLogOpenHandler{locateLogUsecase: locateLogUsecase, defaultLength: defaultLength}
}

func (h *GetLogOpenHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(rw, "invalid log id", http.StatusBadRequest)
		return
	}
	out, err := h.locateLogUsecase.Exec(ctx, usecase.LocateLogUsecaseInput{LogID: model.LogLineID(id)})
	var domainErr *model.Error
	if errors.Is(err, model.ErrNotFound) && errors.As(err, &domainErr) {
		http.Error(rw, domainErr.Message(), http.StatusNotFound)
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "locate log", slog.Any("err", err))
		http.Error(rw, "failed to locate the log", http.StatusInternalServerError)
		return
	}
	m := out.Mark
	to := schedulerURLAt(m.At, h.defaultLength)
	if m.HasGoroutine() {
		var from time.Time
		if m.Line.HasDuration {
			from = m.At.Add(-m.Line.Duration)
		}
		to = goroutineURL(m.Goroutine, m.At, from)
	}
	http.Redirect(rw, r, withLog(to, m.Line.ID), http.StatusSeeOther)
}
