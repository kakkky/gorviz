package handler

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const issueStreamURL = "/issues/detail/stream"

// issueURL は Issue 1 つの画面。fingerprint はパスやスペースを含むので、パスではなく ?id= で渡す
func issueURL(fingerprint, rangeKey string) string {
	return "/issues/detail?" + url.Values{"id": {fingerprint}, "range": {rangeKey}}.Encode()
}

// GetIssueHandler は Issue 1 つの枠だけを返し、中身は SSE で届く。期間は Overview と同じ ?range= と、拡大した ?from=&to=
type GetIssueHandler struct {
	targetURL string
}

func NewGetIssueHandler(targetURL string) *GetIssueHandler {
	return &GetIssueHandler{targetURL: targetURL}
}

func (h *GetIssueHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	window := overviewWindowOf(r)
	q := window.query()
	q.Set("id", id)
	vm := pages.IssueViewModel{TargetURL: h.targetURL, StreamURL: issueStreamURL + "?" + q.Encode()}
	vm.Picker = timeRangePicker(window, time.Now(), partials.IssueRangeID, "/issues/detail", url.Values{"id": {id}, "range": {window.Key}}, func(key string) string { return issueURL(id, key) })
	rw.Header().Set("Content-Type", htmlContentType)
	if err := pages.Issue(vm).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render issue", slog.Any("err", err))
	}
}

// GetIssueStreamHandler は Issue 1 つの推移と見つかった回を、refresh ごとに SSE で送る
type GetIssueStreamHandler struct {
	getIssueHistoryUsecase *usecase.GetIssueHistoryUsecase
	interval               time.Duration
}

func NewGetIssueStreamHandler(getIssueHistoryUsecase *usecase.GetIssueHistoryUsecase, interval time.Duration) *GetIssueStreamHandler {
	return &GetIssueStreamHandler{getIssueHistoryUsecase: getIssueHistoryUsecase, interval: interval}
}

func (h *GetIssueStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.URL.Query().Get("id")
	window := overviewWindowOf(r)
	// 画面を開いている間ずっと書き続けるので、http.Server の WriteTimeout をこのリクエストだけ外す
	if err := http.NewResponseController(rw).SetWriteDeadline(time.Time{}); err != nil {
		slog.ErrorContext(ctx, "clear write deadline for issue stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		slog.ErrorContext(ctx, "open issue stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		now := time.Now()
		end, span := window.bounds(now)
		out, err := h.getIssueHistoryUsecase.Exec(ctx, usecase.GetIssueHistoryUsecaseInput{Fingerprint: id, To: end, Span: span, Now: now})
		if err != nil {
			slog.WarnContext(ctx, "get issue history", slog.Any("err", err))
			return
		}
		vm := toIssueDetailViewModel(out, span, now)
		if window.zoomed() {
			vm.ResetURL = issueURL(id, window.Key)
		}
		if err := sse.Write(ctx, partials.IssueStreamUpdate(vm)); err != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func toIssueDetailViewModel(out usecase.GetIssueHistoryUsecaseOutput, span time.Duration, now time.Time) components.IssueDetailViewModel {
	if !out.HasLast {
		return components.IssueDetailViewModel{}
	}
	last := out.Last
	vm := components.IssueDetailViewModel{
		Kept: true, Kind: string(last.Kind), Certainty: string(last.Certainty), Subject: last.Subject, Location: last.Location, Summary: last.Summary,
		Active:    now.Sub(last.SeenAt) <= domain.IssueActiveWindow,
		FirstSeen: out.FirstSeen.Local().Format("01/02 15:04:05") + " (" + formatAge(now.Sub(out.FirstSeen)) + ")",
		LastSeen:  last.SeenAt.Local().Format("01/02 15:04:05") + " (" + formatAge(now.Sub(last.SeenAt)) + ")",
		Count:     out.Summary.Count,
	}
	f := chartFrame{step: out.Step, span: span}
	seen := chartSeries{label: "seen", class: overviewSeriesClass + "1"}
	for _, s := range out.Steps {
		f.steps = append(f.steps, s.At)
		seen.values = append(seen.values, usecase.MetricSample{Value: float64(s.Count), OK: true})
	}
	vm.Range = timeRangeOver(out.From, out.To, span)
	vm.Steps = f.stepsJSON()
	vm.Chart = f.stackedBarChart([]chartSeries{seen}, formatCount)
	for _, o := range out.Occurrences {
		vm.Occurrences = append(vm.Occurrences, components.IssueOccurrenceRowViewModel{
			Time: o.SeenAt.Local().Format("01/02 15:04:05"), Age: formatAge(now.Sub(o.SeenAt)), Summary: o.Summary,
			Replayable: o.Replayable, Links: issueLinks(o.IssueOccurrence),
		})
	}
	vm.MoreOccurrences = out.Summary.Count - len(out.Occurrences)
	return vm
}
