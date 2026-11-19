package handler

import (
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const issuesStreamURL = "/issues/stream"

// issueKinds は Issues で絞り込める種類
var issueKinds = []model.IssueKind{model.IssueLeak, model.IssueCPUWaitSpike, model.IssueMemoryPressure}

// issuesFilter は Issues の期間 (Overview と同じ ?range= と、推移を拡大した ?from=&to=) と ?q= (文字) と ?kind= (種類)
type issuesFilter struct {
	Window   overviewWindow
	RangeKey string
	Query    string
	Kind     model.IssueKind
}

func issuesFilterOf(r *http.Request) issuesFilter {
	f := issuesFilter{Window: overviewWindowOf(r), Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	f.RangeKey = f.Window.Key
	if k := model.IssueKind(r.URL.Query().Get("kind")); slices.Contains(issueKinds, k) {
		f.Kind = k
	}
	return f
}

func (f issuesFilter) query() url.Values {
	q := f.Window.query()
	if f.Query != "" {
		q.Set("q", f.Query)
	}
	if f.Kind != "" {
		q.Set("kind", string(f.Kind))
	}
	return q
}

// GetIssuesHandler は枠だけを返し、一覧は SSE で届く
type GetIssuesHandler struct {
	targetURL string
}

func NewGetIssuesHandler(targetURL string) *GetIssuesHandler {
	return &GetIssuesHandler{targetURL: targetURL}
}

func (h *GetIssuesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	f := issuesFilterOf(r)
	vm := pages.IssuesViewModel{
		TargetURL: h.targetURL, StreamURL: issuesStreamURL + "?" + f.query().Encode(),
	}
	keep := f.query()
	keep.Del("from")
	keep.Del("to")
	vm.Picker = timeRangePicker(f.Window, time.Now(), partials.IssuesRangeID, "/issues", keep, func(key string) string {
		q := maps.Clone(keep)
		q.Set("range", key)
		return "/issues?" + q.Encode()
	})
	vm.Search = components.SearchFormViewModel{
		Action: "/issues", Query: f.Query, Placeholder: "search subject or summary (e.g. a function name)",
		Select: components.SearchSelectViewModel{Name: "kind", All: "all kinds", Selected: string(f.Kind)},
		Keep:   []components.KeptParamViewModel{{Name: "range", Value: f.RangeKey}},
	}
	for _, k := range issueKinds {
		vm.Search.Select.Options = append(vm.Search.Select.Options, string(k))
	}
	rw.Header().Set("Content-Type", htmlContentType)
	if err := pages.Issues(vm).Render(r.Context(), rw); err != nil {
		slog.ErrorContext(r.Context(), "render issues", slog.Any("err", err))
	}
}

// GetIssuesStreamHandler は期間の中に見つかった Issue の一覧を、refresh ごとに SSE で送る
type GetIssuesStreamHandler struct {
	listIssueHistoryUsecase *usecase.ListIssueHistoryUsecase
	interval                time.Duration
}

func NewGetIssuesStreamHandler(listIssueHistoryUsecase *usecase.ListIssueHistoryUsecase, interval time.Duration) *GetIssuesStreamHandler {
	return &GetIssuesStreamHandler{listIssueHistoryUsecase: listIssueHistoryUsecase, interval: interval}
}

func (h *GetIssuesStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := issuesFilterOf(r)
	// 画面を開いている間ずっと書き続けるので、http.Server の WriteTimeout をこのリクエストだけ外す
	if err := http.NewResponseController(rw).SetWriteDeadline(time.Time{}); err != nil {
		slog.ErrorContext(ctx, "clear write deadline for issues stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		slog.ErrorContext(ctx, "open issues stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		now := time.Now()
		end, span := f.Window.bounds(now)
		out, err := h.listIssueHistoryUsecase.Exec(ctx, usecase.ListIssueHistoryUsecaseInput{Now: now, To: end, Span: span, Query: f.Query, Kind: f.Kind})
		if err != nil {
			slog.WarnContext(ctx, "list issue history", slog.Any("err", err))
		} else if err := sse.Write(ctx, partials.IssuesStreamUpdate(toIssueListViewModel(out, f, now))); err != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func toIssueListViewModel(out usecase.ListIssueHistoryUsecaseOutput, f issuesFilter, now time.Time) components.IssueListViewModel {
	vm := components.IssueListViewModel{
		Range:    timeRangeOver(out.From, out.To, spanOf(f.Window.bounds(now))),
		Filtered: f.Query != "" || f.Kind != "",
	}
	if f.Window.zoomed() {
		q := f.query()
		q.Del("from")
		q.Del("to")
		vm.ResetURL = "/issues?" + q.Encode()
	}
	addIssueTrend(&vm, out)
	for _, i := range out.Issues {
		row := toIssueRowViewModel(i, now)
		row.DetailURL = issueURL(i.Latest.Fingerprint, f.RangeKey)
		vm.Issues = append(vm.Issues, row)
	}
	return vm
}

// toIssueRowViewModel は Issue 1 つを、いちばん新しく見つかった 1 回で説明し、そのときの調べに入る先を並べる
func toIssueRowViewModel(i usecase.IssueHistory, now time.Time) components.IssueRowViewModel {
	o := i.Latest
	vm := components.IssueRowViewModel{
		Kind: string(o.Kind), Certainty: string(o.Certainty), Subject: o.Subject, Summary: o.Summary,
		Active: i.Active, Count: i.Count,
		FirstSeen: formatAge(now.Sub(i.FirstSeen)), LastSeen: formatAge(now.Sub(i.LastSeen)),
	}
	peak := slices.Max(i.Trend)
	for _, n := range i.Trend {
		h := "0"
		if peak > 0 && n > 0 {
			h = fmt.Sprintf("%.0f%%", max(float64(n)/float64(peak)*100, 15))
		}
		vm.Trend = append(vm.Trend, h)
	}
	vm.Links = issueLinks(o)
	return vm
}

// issueLinks は Issue が見つかったときの調べに入る先。代表の G があれば Goroutine、CPU 待ちの急増なら Scheduler
func issueLinks(o domain.IssueOccurrence) []components.LinkViewModel {
	var links []components.LinkViewModel
	if o.HasGoroutine {
		links = append(links, components.LinkViewModel{Label: fmt.Sprintf("Diagnose G%d", o.Goroutine), URL: goroutineURL(o.Goroutine, o.At, o.From)})
	}
	// CPU 待ちの急増は録画の区切り 1 つ分で判定するので、その区切りの全体 (At は区切りの真ん中) を再生する
	if o.Kind == model.IssueCPUWaitSpike {
		links = append(links, components.LinkViewModel{Label: "Replay in scheduler", URL: schedulerURLAt(o.At, 2*time.Second)})
	}
	return links
}

// addIssueTrend は期間の推移を、種類ごとに積み上げた棒にする。種類ごとの回数と割合も添える
func addIssueTrend(vm *components.IssueListViewModel, out usecase.ListIssueHistoryUsecaseOutput) {
	f := chartFrame{step: out.Step, span: out.To.Sub(out.From)}
	for _, s := range out.Steps {
		f.steps = append(f.steps, s.At)
	}
	total := 0
	for _, n := range out.Totals {
		total += n
	}
	var series []chartSeries
	for i, k := range issueKinds {
		cs := chartSeries{label: string(k), class: fmt.Sprintf("%s%d", overviewSeriesClass, i+1)}
		for _, s := range out.Steps {
			cs.values = append(cs.values, usecase.MetricSample{Value: float64(s.Counts[k]), OK: true})
		}
		series = append(series, cs)
		if n := out.Totals[k]; n > 0 {
			vm.Kinds = append(vm.Kinds, components.IssueKindTotalViewModel{Kind: string(k), Count: n, Percent: fmt.Sprintf("%.0f%%", float64(n)/float64(total)*100), Class: cs.class})
		}
	}
	vm.Steps = f.stepsJSON()
	vm.Chart = f.stackedBarChart(series, formatCount)
	vm.Total = total
}
