package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/kakkky/hotwire-go/turbo"

	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/presentation/view/components"
	"github.com/kakkky/gorviz/presentation/view/pages"
	"github.com/kakkky/gorviz/presentation/view/partials"
	"github.com/kakkky/gorviz/usecase"
)

const exploreLogLimit = 300

var logLevels = []domain.LogLevel{domain.LogLevelError, domain.LogLevelWarn, domain.LogLevelInfo, domain.LogLevelDebug}

type GetExploreHandler struct {
	listLogsUsecase *usecase.ListLogsUsecase
	targetURL       string
}

func NewGetExploreHandler(listLogsUsecase *usecase.ListLogsUsecase, targetURL string) *GetExploreHandler {
	return &GetExploreHandler{listLogsUsecase: listLogsUsecase, targetURL: targetURL}
}

func (h *GetExploreHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	ctx := r.Context()
	query, level := r.URL.Query().Get("q"), domain.LogLevel(r.URL.Query().Get("level"))
	out, err := h.listLogsUsecase.Exec(ctx, usecase.ListLogsUsecaseInput{Query: query, Level: level, Limit: exploreLogLimit})
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
		page = pages.Explore(pages.ExploreViewModel{TargetURL: h.targetURL, Query: query, Level: string(level), Levels: levels, Logs: logs})
	}
	if err := page.Render(ctx, rw); err != nil {
		slog.ErrorContext(ctx, "render explore", slog.Any("err", err))
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
		for _, lv := range append(logLevels, domain.LogLevelUnknown) {
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

	for _, l := range out.Lines {
		vm.Lines = append(vm.Lines, components.LogRowViewModel{
			Time:       l.Line.At.Local().Format("15:04:05.000"),
			Level:      levelLabel(l.Line.Level),
			LevelClass: levelClass(l.Line.Level),
			Message:    l.Line.Message,
			Source:     l.Line.Source,
			Replayable: l.Replayable,
			ReplayURL:  logReplayURL(l.Line.ID, defaultReplayHalfWindow),
			Exact:      l.Line.HasTraceSeq,
		})
	}
	return vm
}

func levelLabel(lv domain.LogLevel) string {
	if lv == domain.LogLevelUnknown {
		return "-"
	}
	return strings.ToUpper(string(lv))
}

func levelClass(lv domain.LogLevel) string {
	if lv == domain.LogLevelUnknown {
		return "unknown"
	}
	return string(lv)
}
