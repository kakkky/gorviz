package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
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
	for _, rg := range overviewRanges {
		vm.Ranges = append(vm.Ranges, pages.RangeLinkViewModel{Label: rg.Key, URL: "/?" + url.Values{"range": {rg.Key}}.Encode(), Active: rg.Key == window.Key && !window.zoomed()})
	}
	if err := pages.Overview(vm).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render overview", slog.Any("err", err))
	}
}

// schedulerLengths は Scheduler で選べる再生の長さ。録画の区切り (agent の -chunk、既定 2 秒) より長くは切り出せない
var schedulerLengths = []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}

// schedulerLengthOf は ?length= を読む。選べる長さでなければ fallback
func schedulerLengthOf(r *http.Request, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(r.URL.Query().Get("length"))
	if err == nil && slices.Contains(schedulerLengths, d) {
		return d
	}
	return fallback
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
	length := schedulerLengthOf(r, h.defaultLength)
	latest := "/scheduler?" + url.Values{"length": {length.String()}}.Encode()
	vm := pages.SchedulerViewModel{TargetURL: h.targetURL, LatestURL: latest}
	in := usecase.GetSchedulerReplayUsecaseInput{Length: length}
	at, atErr := unixNanoParam(r, "at")
	if atErr == nil {
		vm.Past, in.At = true, at
	}
	out, err := h.getSchedulerReplayUsecase.Exec(ctx, in)
	var domainErr *model.Error
	switch {
	case errors.Is(err, model.ErrNotFound) && errors.As(err, &domainErr):
		vm.Error = domainErr.Message()
	case err != nil:
		slog.ErrorContext(ctx, "get scheduler replay", slog.Any("err", err))
		vm.Error = "failed to build the replay"
	default:
		if vm.ReplayJSON, err = newGMPReplayJSON(out.Timeline, out.Leaked, out.StartedAt).marshal(); err != nil {
			slog.ErrorContext(ctx, "encode gmp replay", slog.Any("err", err))
			vm.Error = "failed to build the replay"
		}
	}
	for _, l := range schedulerLengths {
		q := url.Values{"length": {l.String()}}
		if atErr == nil {
			q.Set("at", strconv.FormatInt(at.UnixNano(), 10))
		}
		vm.Lengths = append(vm.Lengths, pages.RangeLinkViewModel{Label: l.String(), URL: "/scheduler?" + q.Encode(), Active: l == length})
	}
	rw.Header().Set("Content-Type", htmlContentType)
	if err := pages.Scheduler(vm).Render(ctx, rw); err != nil {
		slog.ErrorContext(ctx, "render scheduler", slog.Any("err", err))
	}
}

// schedulerURLAt は Scheduler で、at を真ん中にした length の区間を再生する URL
func schedulerURLAt(at time.Time, length time.Duration) string {
	return "/scheduler?" + url.Values{"at": {strconv.FormatInt(at.UnixNano(), 10)}, "length": {length.String()}}.Encode()
}
