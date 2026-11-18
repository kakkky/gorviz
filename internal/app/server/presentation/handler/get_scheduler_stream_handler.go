package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const schedulerStreamURL = "/scheduler/stream"

// GetSchedulerStreamHandler は Scheduler で最新を見ているあいだ、新しい窓が録れるたびに SSE で送る
type GetSchedulerStreamHandler struct {
	getSchedulerReplayUsecase *usecase.GetSchedulerReplayUsecase
	interval                  time.Duration
	defaultLength             time.Duration
}

func NewGetSchedulerStreamHandler(getSchedulerReplayUsecase *usecase.GetSchedulerReplayUsecase, interval, defaultLength time.Duration) *GetSchedulerStreamHandler {
	return &GetSchedulerStreamHandler{getSchedulerReplayUsecase: getSchedulerReplayUsecase, interval: interval, defaultLength: defaultLength}
}

func (h *GetSchedulerStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	length := replayWindowOf(r, h.defaultLength).Length
	// 画面を開いている間ずっと書き続けるので、http.Server の WriteTimeout をこのリクエストだけ外す
	if err := http.NewResponseController(rw).SetWriteDeadline(time.Time{}); err != nil {
		slog.ErrorContext(ctx, "clear write deadline for scheduler stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		slog.ErrorContext(ctx, "open scheduler stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		out, err := h.getSchedulerReplayUsecase.Exec(ctx, usecase.GetSchedulerReplayUsecaseInput{Length: length})
		// 録画がまだない・新しい窓がまだ録れていないときは送らない (同じ窓を読み直すと再生が頭に戻る)
		if err != nil || !out.StartedAt.After(last) {
			continue
		}
		last = out.StartedAt
		replay := newGMPReplayJSON(out.Timeline, out.Leaked, out.StartedAt)
		addReplayLogs(&replay, out.Logs, out.StartedAt, 0, false)
		json, err := replay.marshal()
		if err != nil {
			slog.ErrorContext(ctx, "encode gmp replay", slog.Any("err", err))
			continue
		}
		if err := sse.Write(ctx, partials.ReplayStreamUpdate(json)); err != nil {
			return
		}
	}
}
