package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/pages"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/partials"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const logsLimit = 300

var logLevels = []model.LogLevel{model.LogLevelError, model.LogLevelWarn, model.LogLevelInfo, model.LogLevelDebug}

type GetLogsHandler struct {
	listLogsUsecase *usecase.ListLogsUsecase
	targetURL       string
}

func NewGetLogsHandler(listLogsUsecase *usecase.ListLogsUsecase, targetURL string) *GetLogsHandler {
	return &GetLogsHandler{listLogsUsecase: listLogsUsecase, targetURL: targetURL}
}

func (h *GetLogsHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	ctx := r.Context()
	query, level := r.URL.Query().Get("q"), model.LogLevel(r.URL.Query().Get("level"))
	in := usecase.ListLogsUsecaseInput{Query: query, Level: level, Limit: logsLimit}
	// from / to (Unix ナノ秒) は Goroutine や Overview から開いたときに付く。since / until は画面の時刻の入力欄
	in.From, _ = unixNanoParam(r, "from")
	in.To, _ = unixNanoParam(r, "to")
	var timeErr error
	if t, ok, err := localTimeParam(r, "since"); ok {
		in.From = t
	} else if err != nil {
		timeErr = err
	}
	if t, ok, err := localTimeParam(r, "until"); ok {
		in.To = t
	} else if err != nil {
		timeErr = err
	}
	out, err := h.listLogsUsecase.Exec(ctx, in)
	if err != nil {
		slog.ErrorContext(ctx, "list logs", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	logs := toLogListViewModel(out)

	var page templ.Component = partials.LogListFrame(logs)
	if !turbo.IsFrameRequest(r) {
		levels := make([]string, len(logLevels))
		for i, lv := range logLevels {
			levels[i] = string(lv)
		}
		vm := pages.LogsViewModel{TargetURL: h.targetURL, Query: query, Level: string(level), Levels: levels, Logs: logs, TimeZone: timeZoneLabel()}
		if !in.From.IsZero() {
			vm.Since = in.From.Local().Format(localTimeInputLayout)
		}
		if !in.To.IsZero() {
			vm.Until = in.To.Local().Format(localTimeInputLayout)
		}
		if timeErr != nil {
			vm.TimeError = timeErr.Error()
		}
		page = pages.Logs(vm)
	}
	if err := page.Render(ctx, rw); err != nil {
		slog.ErrorContext(ctx, "render logs", slog.Any("err", err))
	}
}

func toLogListViewModel(out usecase.ListLogsUsecaseOutput) components.LogListViewModel {
	vm := components.LogListViewModel{Total: out.Total, Shown: len(out.Lines)}

	peak := 1
	for _, b := range out.Histogram {
		total := 0
		for _, c := range b.Counts {
			total += c
		}
		peak = max(peak, total)
	}
	for _, b := range out.Histogram {
		bar := components.LogHistogramBarViewModel{}
		var parts []string
		for _, lv := range append(logLevels, model.LogLevelUnknown) {
			n := b.Counts[lv]
			if n == 0 {
				continue
			}
			bar.Segments = append(bar.Segments, components.LogHistogramSegmentViewModel{
				Class:  "level-bg-" + levelClass(lv),
				Height: fmt.Sprintf("%.1f%%", float64(n)/float64(peak)*100),
			})
			parts = append(parts, fmt.Sprintf("%s %d", levelLabel(lv), n))
		}
		bar.Title = b.Start.Format(time.TimeOnly) + "  " + strings.Join(parts, ", ")
		vm.Histogram = append(vm.Histogram, bar)
	}

	sources := map[string]bool{}
	for _, l := range out.Lines {
		sources[l.Line.Source] = true
		vm.Lines = append(vm.Lines, components.LogRowViewModel{
			Time:       l.Line.At.Local().Format("15:04:05.000"),
			Level:      levelLabel(l.Line.Level),
			LevelClass: levelClass(l.Line.Level),
			Message:    l.Line.Message,
			Source:     l.Line.Source,
			Replayable: l.Replayable,
			ReplayURL:  logReplayURL(l.Line.ID, defaultReplayHalfWindow),
			Exact:      l.Line.HasTraceSeq,
			Duration:   formatDuration(l.Line),
			OverviewURL: "/?" + url.Values{
				"from": {strconv.FormatInt(l.Line.At.Add(-logOverviewHalfWindow).UnixMilli(), 10)},
				"to":   {strconv.FormatInt(l.Line.At.Add(logOverviewHalfWindow).UnixMilli(), 10)},
			}.Encode(),
		})
	}
	vm.ShowSource = len(sources) > 1
	return vm
}

// logOverviewHalfWindow は、ログの行から Overview を開くときに見る前後の長さ
const logOverviewHalfWindow = 5 * time.Minute

func levelLabel(lv model.LogLevel) string {
	if lv == model.LogLevelUnknown {
		return "-"
	}
	return strings.ToUpper(string(lv))
}

func levelClass(lv model.LogLevel) string {
	if lv == model.LogLevelUnknown {
		return "unknown"
	}
	return string(lv)
}
