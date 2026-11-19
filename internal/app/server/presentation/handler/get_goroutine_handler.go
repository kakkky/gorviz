package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// goroutineReplayHalf は API (MCP の診断) で見る、At の前後の長さ。画面は再生の長さ (?length=) の半分ずつを見る
const goroutineReplayHalf = 250 * time.Millisecond

type GetGoroutineHandler struct {
	getGoroutineUsecase       *usecase.GetGoroutineUsecase
	getSchedulerReplayUsecase *usecase.GetSchedulerReplayUsecase
	listIssuesUsecase         *usecase.ListIssuesUsecase
	targetURL                 string
	defaultLength             time.Duration
}

func NewGetGoroutineHandler(getGoroutineUsecase *usecase.GetGoroutineUsecase, getSchedulerReplayUsecase *usecase.GetSchedulerReplayUsecase, listIssuesUsecase *usecase.ListIssuesUsecase, targetURL string, defaultLength time.Duration) *GetGoroutineHandler {
	return &GetGoroutineHandler{getGoroutineUsecase: getGoroutineUsecase, getSchedulerReplayUsecase: getSchedulerReplayUsecase, listIssuesUsecase: listIssuesUsecase, targetURL: targetURL, defaultLength: defaultLength}
}

// goroutineURL は Goroutine 画面への URL。時刻は Unix ナノ秒で渡す。from を省くと at の前後を見る
func goroutineURL(g model.GoroutineID, at, from time.Time) string {
	q := url.Values{"at": {strconv.FormatInt(at.UnixNano(), 10)}}
	if !from.IsZero() && from.Before(at) {
		q.Set("from", strconv.FormatInt(from.UnixNano(), 10))
	}
	return fmt.Sprintf("/goroutines/%d?%s", g, q.Encode())
}

func (h *GetGoroutineHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	ctx := r.Context()
	vm := pages.GoroutineViewModel{TargetURL: h.targetURL}
	render := func() {
		if err := pages.Goroutine(vm).Render(ctx, rw); err != nil {
			slog.ErrorContext(ctx, "render goroutine", slog.Any("err", err))
		}
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	w := replayWindowOf(r, h.defaultLength)
	length := w.Length
	var skipped time.Duration
	latest := fmt.Sprintf("/goroutines/%d?", id) + url.Values{"length": {length.String()}}.Encode()
	live := err == nil && !w.targeted()
	// 時刻の指定がなければ最新の窓を、再生の続き・前 (after / before) ならその区間を、真ん中で診断する
	if err == nil && !w.HasAt {
		win, werr := h.getSchedulerReplayUsecase.Exec(ctx, w.input())
		// 再生の続きが録画の終わりを越えたら、最新を追う画面に移る (再生していたなら、そのまま再生を続ける)
		if errors.Is(werr, model.ErrNotFound) && !w.After.IsZero() {
			to := latest
			if r.URL.Query().Get("autoplay") != "" {
				to += "&autoplay=1"
			}
			http.Redirect(rw, r, to, http.StatusSeeOther)
			return
		}
		if werr == nil {
			w.At, w.HasAt = win.StartedAt.Add(win.Timeline.Window/2), true
			skipped = skippedBefore(w, win.StartedAt)
		}
	}
	at := w.At
	if err != nil || !w.HasAt {
		vm.Error = "invalid goroutine id or time, or nothing recorded yet"
		render()
		return
	}
	// 診断する区間も再生と同じく、At を真ん中にした再生の長さ
	half := length / 2
	in := usecase.GetGoroutineUsecaseInput{Goroutine: model.GoroutineID(id), At: at, From: at.Add(-half), To: at.Add(half), ReplayHalf: half}
	// from があるのは所要時間のあるログから来たとき。そのリクエストの区間 [from, at] を見る
	if from, err := unixNanoParam(r, "from"); err == nil && from.Before(at) {
		in.From, in.To = from, at
	}

	out, err := h.getGoroutineUsecase.Exec(ctx, in)
	var domainErr *model.Error
	if errors.As(err, &domainErr) && errors.Is(err, model.ErrInvalidArgument) {
		vm.Error = domainErr.Message()
		render()
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "get goroutine", slog.Any("err", err))
		vm.Error = "failed to diagnose the goroutine"
		render()
		return
	}

	vm.Goroutine, vm.GoroutineID = fmt.Sprintf("G%d", out.Goroutine), strconv.FormatInt(int64(out.Goroutine), 10)
	vm.Entry = out.Entry
	vm.Interval = timeRange(out.From, out.To)
	vm.At = out.At.Local().Format("15:04:05.000000")
	vm.Leaked = out.HasDump && out.Dump.Leaked
	if out.HasPrimary {
		vm.Primary, vm.Certainty = string(out.Primary), string(out.Certainty)
	}
	vm.Function, vm.Location, vm.WaitReason = dash(out.Function), dash(out.Location), dash(out.WaitReason)
	if out.HasLongest {
		vm.Wake = goroutineWake(out.Longest)
	}
	vm.CreatedBy = "-"
	if out.HasDump && out.Dump.CreatedBy != "" {
		vm.CreatedBy = string(out.Dump.Origin())
	}
	vm.Length = length.String()
	vm.LatestURL = latest
	if live {
		vm.StreamURL = schedulerStreamURL + "?" + url.Values{"length": {length.String()}}.Encode()
	}
	if issues, err := h.listIssuesUsecase.Exec(ctx, usecase.ListIssuesUsecaseInput{Now: time.Now()}); err == nil {
		for _, i := range issues.Issues {
			sameGoroutine := i.HasGoroutine && i.Goroutine == out.Goroutine
			sameOrigin := i.Kind == model.IssueLeak && out.HasDump && i.Subject == string(out.Dump.Origin())
			if sameGoroutine || sameOrigin {
				vm.Issues = append(vm.Issues, components.LinkViewModel{Label: fmt.Sprintf("Issue: %s — %s", i.Kind, i.Summary), URL: issueURL(domain.IssueFingerprint(i), overviewDefaultRange)})
			}
		}
	}

	if !out.HasReplay {
		vm.NoReplay = "the trace at this time is not recorded (dropped from the ring buffer, between recorded chunks, or outside the intermittent recording)"
		render()
		return
	}
	replay := newGMPReplayJSON(out.Replay, out.Leaked, out.ReplayStartedAt)
	replay.SkippedNs = int64(skipped)
	vm.Picker = replayPicker(r, out.At, true, length, out.ReplayStartedAt, out.ReplayStartedAt.Add(out.Replay.Window))
	vm.Picker.RangeID, vm.Picker.Live = partials.ReplayRangeID, live
	replay.Focus = &gmpReplayFocusJSON{AtNs: int64(out.ReplayFocus), Goroutine: int64(out.Goroutine)}
	// 診断の場所と woken by が指す 1 回を、再生の帯で枠で囲む
	if out.HasLongest {
		replay.Longest = &gmpReplayStretchJSON{
			Goroutine: int64(out.Goroutine), Cause: string(out.Primary),
			StartNs: int64(max(out.Longest.From.Sub(out.ReplayStartedAt), 0)), EndNs: int64(min(out.Longest.To.Sub(out.ReplayStartedAt), out.Replay.Window)),
		}
	}
	if out.StateSinceCause != "" {
		replay.Before = &gmpReplayBeforeJSON{Goroutine: int64(out.Goroutine), Cause: string(out.StateSinceCause), RecordedFromNs: out.RecordedFrom.UnixNano()}
		if out.HasStateSince {
			q := url.Values{"length": {length.String()}}
			replay.Before.SinceNs = out.StateSince.UnixNano()
			replay.Before.URL = goroutineURL(out.Goroutine, out.StateSince, time.Time{}) + "&" + q.Encode()
		}
	}
	// Logs から開いたときは、そのログの位置から見せ、この G が書いたかを印にする
	logID, hasLog := logIDParam(r)
	if m, ok := addReplayLogs(&replay, out.ReplayLogs, out.ReplayStartedAt, logID, hasLog); ok && m.HasGoroutine() && m.Goroutine == out.Goroutine {
		replay.Focus = &gmpReplayFocusJSON{AtNs: int64(m.At.Sub(out.ReplayStartedAt)), Goroutine: int64(out.Goroutine)}
	}
	if vm.ReplayJSON, err = replay.marshal(); err != nil {
		slog.ErrorContext(ctx, "encode gmp replay", slog.Any("err", err))
		vm.ReplayJSON = ""
		vm.NoReplay = "failed to build the replay"
	}
	render()
}

// goroutineWake は一番長い待ちから誰が起こしたか。待ち (waiting) でなければ nil
func goroutineWake(s usecase.GoroutineStretch) *pages.GoroutineWakeViewModel {
	switch {
	case s.HasWake && s.Waker != 0:
		w := &pages.GoroutineWakeViewModel{
			Kind: "goroutine", Goroutine: fmt.Sprintf("G%d", s.Waker), URL: goroutineURL(s.Waker, s.To, s.From),
			Entry: s.WakerEntry, Function: s.WakerFunction, Location: s.WakerLocation,
		}
		if s.HasWakerPrimary {
			w.BusyCause = string(s.WakerPrimary)
			busy := s.WakerBreakdown.Durations[s.WakerPrimary]
			w.BusyFor = domain.FormatShortDuration(busy)
			if wait := s.To.Sub(s.From); wait > 0 {
				w.BusyShare = fmt.Sprintf("%d%%", int(float64(busy)/float64(wait)*100+0.5))
			}
			w.BusyFunction, w.BusyLocation = s.WakerBusyFunction, s.WakerBusyLocation
		}
		return w
	case s.HasWake:
		return &pages.GoroutineWakeViewModel{Kind: "runtime"}
	case !s.Ended && s.WaitReason != "":
		return &pages.GoroutineWakeViewModel{Kind: "waiting"}
	}
	return nil
}

// unixMilliParam は Overview・Logs の期間 (グラフを拡大したときの from / to) の Unix ミリ秒
func unixMilliParam(r *http.Request, key string) (time.Time, error) {
	n, err := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(n), nil
}

func unixNanoParam(r *http.Request, key string) (time.Time, error) {
	n, err := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, n), nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func formatDuration(l model.LogLine) string {
	if !l.HasDuration {
		return ""
	}
	return l.Duration.Round(time.Millisecond).String()
}
