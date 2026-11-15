package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const defaultReplayHalfWindow = 250 * time.Millisecond

// 再生区間の選択肢 (クリックしたログの前後それぞれの長さ)
var replayHalfWindows = []time.Duration{50 * time.Millisecond, defaultReplayHalfWindow, time.Second}

type GetLogReplayHandler struct {
	getLogReplayUsecase *usecase.GetLogReplayUsecase
}

func NewGetLogReplayHandler(getLogReplayUsecase *usecase.GetLogReplayUsecase) *GetLogReplayHandler {
	return &GetLogReplayHandler{getLogReplayUsecase: getLogReplayUsecase}
}

func (h *GetLogReplayHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.renderError(rw, r, "invalid log id")
		return
	}
	half := defaultReplayHalfWindow
	if d, err := time.ParseDuration(r.URL.Query().Get("half")); err == nil && d > 0 && d <= 5*time.Second {
		half = d
	}

	out, err := h.getLogReplayUsecase.Exec(ctx, usecase.GetLogReplayUsecaseInput{LogID: model.LogLineID(id), Before: half, After: half})
	var domainErr *model.Error
	if errors.Is(err, model.ErrNotFound) && errors.As(err, &domainErr) {
		h.renderError(rw, r, domainErr.Message())
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "get log replay", slog.Any("err", err))
		h.renderError(rw, r, "failed to build the replay")
		return
	}

	replay := newGMPReplayJSON(out.Timeline, out.Leaked, out.WindowStartedAt)
	focusG := int64(-1)
	if out.Focus.Exact {
		focusG = int64(out.Focus.Goroutine)
	}
	replay.Focus = &gmpReplayFocusJSON{AtNs: int64(out.Focus.At), Goroutine: focusG, Exact: out.Focus.Exact}
	for _, m := range out.WindowLogs {
		g := int64(-1)
		if m.Exact {
			g = int64(m.Goroutine)
		}
		replay.Logs = append(replay.Logs, gmpReplayLogJSON{AtNs: int64(m.At), Text: m.Line.Message, Level: string(m.Line.Level), Goroutine: g})
	}
	replayJSON, err := replay.marshal()
	if err != nil {
		slog.ErrorContext(ctx, "encode gmp replay", slog.Any("err", err))
		h.renderError(rw, r, "failed to build the replay")
		return
	}

	focus := fmt.Sprintf("located by the log's timestamp at %s; the goroutine that wrote it is unknown (plug gorviz into your logger with gorviz.NewSlogHandler or a logger/*adapter package to identify it)", out.WindowStartedAt.Add(out.Focus.At).Local().Format("15:04:05.000000"))
	if out.Focus.Inferred {
		focus = fmt.Sprintf("INFERENCE: G%d (%s) was the only goroutine running at the log's timestamp, so it probably wrote the log (plug gorviz into your logger with gorviz.NewSlogHandler or a logger/*adapter package to identify it exactly)", out.Focus.Goroutine, model.ShortFunctionName(out.Timeline.Entries[out.Focus.Goroutine]))
	}
	if out.Focus.Exact {
		focus = fmt.Sprintf("written by G%d (%s), located exactly through the trace", out.Focus.Goroutine, model.ShortFunctionName(out.Timeline.Entries[out.Focus.Goroutine]))
	}
	vm := partials.LogReplayViewModel{
		Duration:   formatDuration(out.Line),
		Time:       out.Line.At.Local().Format("15:04:05.000"),
		Level:      levelLabel(out.Line.Level),
		LevelClass: levelClass(out.Line.Level),
		Message:    out.Line.Message,
		Focus:      focus,
		ReplayJSON: replayJSON,
	}
	if out.Focus.Exact || out.Focus.Inferred {
		at := out.WindowStartedAt.Add(out.Focus.At)
		var from time.Time
		if out.Line.HasDuration {
			from = at.Add(-out.Line.Duration)
		}
		vm.GoroutineURL = goroutineURL(out.Focus.Goroutine, at, from)
		vm.GoroutineLabel = fmt.Sprintf("diagnose G%d", out.Focus.Goroutine)
		if out.Focus.Inferred {
			vm.GoroutineLabel += " (INFERENCE)"
		}
	}
	for _, d := range replayHalfWindows {
		vm.WindowLinks = append(vm.WindowLinks, partials.LogReplayWindowLinkViewModel{Label: "±" + d.String(), URL: logReplayURL(out.Line.ID, d), Active: d == half})
	}
	if err := partials.LogReplayFrame(vm).Render(ctx, rw); err != nil {
		slog.ErrorContext(ctx, "render log replay", slog.Any("err", err))
	}
}

func (h *GetLogReplayHandler) renderError(rw http.ResponseWriter, r *http.Request, msg string) {
	if err := partials.LogReplayFrame(partials.LogReplayViewModel{Error: msg}).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render log replay", slog.Any("err", err))
	}
}

func logReplayURL(id model.LogLineID, half time.Duration) string {
	return fmt.Sprintf("/logs/%d/replay?half=%s", id, half)
}
