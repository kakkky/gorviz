package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// GetOverviewHandler は枠だけを返し、中身は SSE で届く。?range= の期間を見る
type GetOverviewHandler struct {
	targetURL string
}

func NewGetOverviewHandler(targetURL string) *GetOverviewHandler {
	return &GetOverviewHandler{targetURL: targetURL}
}

func (h *GetOverviewHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	window := overviewWindowOf(r)
	rw.Header().Set("Content-Type", htmlContentType)
	vm := pages.OverviewViewModel{TargetURL: h.targetURL, StreamURL: overviewStreamURLFor(window)}
	vm.Picker = timeRangePicker(window, time.Now(), partials.OverviewRangeID, "/", url.Values{"range": {window.Key}}, func(key string) string { return "/?" + url.Values{"range": {key}}.Encode() })
	if err := pages.Overview(vm).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render overview", slog.Any("err", err))
	}
}

// GetSchedulerHandler は録画を ?length= の長さだけ切り出して返す。?at= (Unix ナノ秒) がなければ最新の区切りの末尾を、
// あれば (Issue から飛んできたとき) その時刻を真ん中にした区間を返す。勝手に新しい録画へ差し替えず、最新は開き直して見る
type GetSchedulerHandler struct {
	getSchedulerReplayUsecase *usecase.GetSchedulerReplayUsecase
	targetURL                 string
	defaultLength             time.Duration
}

func NewGetSchedulerHandler(getSchedulerReplayUsecase *usecase.GetSchedulerReplayUsecase, targetURL string, defaultLength time.Duration) *GetSchedulerHandler {
	return &GetSchedulerHandler{getSchedulerReplayUsecase: getSchedulerReplayUsecase, targetURL: targetURL, defaultLength: defaultLength}
}

func (h *GetSchedulerHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w := replayWindowOf(r, h.defaultLength)
	length := w.Length
	latestLength := length
	if !slices.Contains(replayLengths, latestLength) {
		latestLength = h.defaultLength
	}
	latest := "/scheduler?" + url.Values{"length": {latestLength.String()}}.Encode()
	vm := pages.SchedulerViewModel{TargetURL: h.targetURL, LatestURL: latest}
	vm.Past = w.targeted()
	if !vm.Past {
		vm.StreamURL = schedulerStreamURL + "?" + url.Values{"length": {length.String()}}.Encode()
	}
	vm.Picker = replayPicker(r, w.At, w.HasAt, length, time.Time{}, time.Time{})
	vm.Picker.Text = "not recorded"
	out, err := h.getSchedulerReplayUsecase.Exec(ctx, w.input())
	// 再生の続きが録画の終わりを越えたら、最新を追う画面に移る (再生していたなら、そのまま再生を続ける)
	if errors.Is(err, model.ErrNotFound) && !w.After.IsZero() {
		q := url.Values{"length": {latestLength.String()}}
		if r.URL.Query().Get("autoplay") != "" {
			q.Set("autoplay", "1")
		}
		http.Redirect(rw, r, "/scheduler?"+q.Encode(), http.StatusSeeOther)
		return
	}
	var domainErr *model.Error
	switch {
	case errors.Is(err, model.ErrNotFound) && !w.Before.IsZero():
		vm.Error = "no earlier recording: this is the oldest trace gorviz still keeps"
	case errors.Is(err, model.ErrNotFound) && errors.As(err, &domainErr):
		vm.Error = domainErr.Message()
	case err != nil:
		slog.ErrorContext(ctx, "get scheduler replay", slog.Any("err", err))
		vm.Error = "failed to build the replay"
	default:
		replay := newGMPReplayJSON(out.Timeline, out.Leaked, out.StartedAt)
		replay.SkippedNs = int64(skippedBefore(w, out.StartedAt))
		mid := out.StartedAt.Add(out.Timeline.Window / 2)
		vm.Picker = replayPicker(r, mid, vm.Past, length, out.StartedAt, out.StartedAt.Add(out.Timeline.Window))
		vm.Picker.RangeID, vm.Picker.Live = partials.ReplayRangeID, !vm.Past
		logID, hasLog := logIDParam(r)
		if m, ok := addReplayLogs(&replay, out.Logs, out.StartedAt, logID, hasLog); ok {
			replay.Focus = &gmpReplayFocusJSON{AtNs: int64(m.At.Sub(out.StartedAt)), Goroutine: -1}
		}
		if vm.ReplayJSON, err = replay.marshal(); err != nil {
			slog.ErrorContext(ctx, "encode gmp replay", slog.Any("err", err))
			vm.Error = "failed to build the replay"
		}
	}
	vm.Length = length.String()
	rw.Header().Set("Content-Type", htmlContentType)
	if err := pages.Scheduler(vm).Render(ctx, rw); err != nil {
		slog.ErrorContext(ctx, "render scheduler", slog.Any("err", err))
	}
}
