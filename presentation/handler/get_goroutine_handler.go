package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/presentation/view/pages"
	"github.com/kakkky/gorviz/usecase"
)

const goroutineReplayHalf = 250 * time.Millisecond

type GetGoroutineHandler struct {
	getGoroutineUsecase *usecase.GetGoroutineUsecase
	listIssuesUsecase   *usecase.ListIssuesUsecase
	targetURL           string
}

func NewGetGoroutineHandler(getGoroutineUsecase *usecase.GetGoroutineUsecase, listIssuesUsecase *usecase.ListIssuesUsecase, targetURL string) *GetGoroutineHandler {
	return &GetGoroutineHandler{getGoroutineUsecase: getGoroutineUsecase, listIssuesUsecase: listIssuesUsecase, targetURL: targetURL}
}

// goroutineURL は Goroutine 画面への URL。時刻は Unix ナノ秒で渡す。from を省くと at の前後を見る
func goroutineURL(g domain.GoroutineID, at, from time.Time) string {
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
	at, atErr := unixNanoParam(r, "at")
	if err != nil || atErr != nil {
		vm.Error = "invalid goroutine id or time"
		render()
		return
	}
	in := usecase.GetGoroutineUsecaseInput{Goroutine: domain.GoroutineID(id), At: at, From: at.Add(-goroutineReplayHalf), To: at.Add(goroutineReplayHalf), ReplayHalf: goroutineReplayHalf}
	// from があるのは所要時間のあるログから来たとき。そのリクエストの区間 [from, at] を見る
	if from, err := unixNanoParam(r, "from"); err == nil && from.Before(at) {
		in.From, in.To = from, at
	}

	out, err := h.getGoroutineUsecase.Exec(ctx, in)
	var domainErr *domain.Error
	if errors.As(err, &domainErr) && errors.Is(err, domain.ErrInvalidArgument) {
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

	vm.Goroutine = fmt.Sprintf("G%d", out.Goroutine)
	vm.Entry = out.Entry
	vm.Interval = fmt.Sprintf("%s – %s (%s)", out.From.Local().Format("15:04:05.000"), out.To.Local().Format("15:04:05.000"), out.To.Sub(out.From).Round(time.Millisecond))
	vm.At = out.At.Local().Format("15:04:05.000000")
	vm.Leaked = out.HasDump && out.Dump.Leaked
	if out.HasPrimary {
		vm.Primary, vm.Certainty = string(out.Primary), string(out.Certainty)
	}
	vm.Function, vm.Location, vm.WaitReason = dash(out.Function), dash(out.Location), dash(out.WaitReason)
	vm.CreatedBy = "-"
	if out.HasDump && out.Dump.CreatedBy != "" {
		vm.CreatedBy = string(out.Dump.Origin())
	}
	vm.Unrecorded = out.Breakdown.Unrecorded.Round(time.Microsecond).String()
	total := out.To.Sub(out.From)
	for _, c := range domain.PrimaryCauses {
		d := out.Breakdown.Durations[c]
		ratio := float64(d) / float64(total)
		vm.Breakdown = append(vm.Breakdown, pages.BreakdownRowViewModel{
			Cause:    string(c),
			Duration: d.Round(time.Microsecond).String(),
			Percent:  fmt.Sprintf("%.0f%%", ratio*100),
			Width:    fmt.Sprintf("%.1f%%", ratio*100),
			Primary:  out.HasPrimary && c == out.Primary && out.Certainty == domain.CertaintyFact,
		})
	}
	vm.LogsURL = logsURL(out.From.Add(-time.Second), out.To.Add(time.Second))
	if issues, err := h.listIssuesUsecase.Exec(ctx, usecase.ListIssuesUsecaseInput{Now: time.Now()}); err == nil {
		for _, i := range issues.Issues {
			sameGoroutine := i.HasGoroutine && i.Goroutine == out.Goroutine
			sameOrigin := i.Kind == domain.IssueLeak && out.HasDump && i.Subject == string(out.Dump.Origin())
			if sameGoroutine || sameOrigin {
				vm.Issues = append(vm.Issues, fmt.Sprintf("%s %s — %s", i.Kind, i.Certainty, i.Summary))
			}
		}
	}

	if !out.HasReplay {
		vm.NoReplay = "the trace at this time is not recorded (outside the retention, or between recorded chunks)"
		render()
		return
	}
	replay := newGMPReplayJSON(out.Replay, out.Leaked, out.ReplayStartedAt)
	replay.Focus = &gmpReplayFocusJSON{AtNs: int64(out.ReplayFocus), Goroutine: int64(out.Goroutine), Exact: true}
	if vm.ReplayJSON, err = replay.marshal(); err != nil {
		slog.ErrorContext(ctx, "encode gmp replay", slog.Any("err", err))
		vm.ReplayJSON = ""
		vm.NoReplay = "failed to build the replay"
	}
	render()
}

func logsURL(from, to time.Time) string {
	return "/logs?" + url.Values{"from": {strconv.FormatInt(from.UnixNano(), 10)}, "to": {strconv.FormatInt(to.UnixNano(), 10)}}.Encode()
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

func formatDuration(l domain.LogLine) string {
	if !l.HasDuration {
		return ""
	}
	return l.Duration.Round(time.Millisecond).String()
}
