package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

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

// logsFilter は Logs の絞り込み。期間は range (今からさかのぼる、Overview と同じ期間の名前。何も指定がなければ既定の期間)、
// または決まった範囲 (from / to の Unix ミリ秒はグラフを拡大したとき、since / until は期間を選ぶ部品の入力欄)
type logsFilter struct {
	Query    string
	Level    model.LogLevel
	RangeKey string
	Span     time.Duration
	From     time.Time
	To       time.Time
	TimeErr  error
}

func logsFilterOf(r *http.Request) logsFilter {
	f := logsFilter{Query: r.URL.Query().Get("q"), Level: model.LogLevel(r.URL.Query().Get("level"))}
	f.From, _ = unixMilliParam(r, "from")
	f.To, _ = unixMilliParam(r, "to")
	if t, ok, err := localTimeParam(r, "since"); ok {
		f.From = t
	} else if err != nil {
		f.TimeErr = err
	}
	if t, ok, err := localTimeParam(r, "until"); ok {
		f.To = t
	} else if err != nil {
		f.TimeErr = err
	}
	if f.fixed() {
		return f
	}
	key, span, ok := logRange(r)
	if !ok {
		key, span, _ = logRangeOf(overviewDefaultRange)
	}
	f.RangeKey, f.Span = key, span
	return f
}

// fixed は決まった範囲を見ているか。そうでなければ今に合わせて動く期間で、refresh ごとに一覧が新しくなる
func (f logsFilter) fixed() bool {
	return !f.From.IsZero() || !f.To.IsZero()
}

func (f logsFilter) input(now time.Time) usecase.ListLogsUsecaseInput {
	in := usecase.ListLogsUsecaseInput{Query: f.Query, Level: f.Level, Limit: logsLimit, From: f.From, To: f.To}
	if !f.fixed() {
		in.From, in.To = now.Add(-f.Span), now
	}
	return in
}

func (f logsFilter) query() url.Values {
	q := url.Values{}
	if f.Query != "" {
		q.Set("q", f.Query)
	}
	if f.Level != "" {
		q.Set("level", string(f.Level))
	}
	if f.RangeKey != "" {
		q.Set("range", f.RangeKey)
	}
	return q
}

// resetURL は決まった範囲 (拡大・期間の入力) を見ているときの、既定の期間に戻る先。今に合わせて動く期間なら空
func (f logsFilter) resetURL() string {
	if !f.fixed() {
		return ""
	}
	q := f.query()
	q.Set("range", overviewDefaultRange)
	return "/logs?" + q.Encode()
}

func (h *GetLogsHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", htmlContentType)
	ctx := r.Context()
	f := logsFilterOf(r)
	now := time.Now()
	in := f.input(now)
	out, err := h.listLogsUsecase.Exec(ctx, in)
	if err != nil {
		slog.ErrorContext(ctx, "list logs", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	levels := make([]string, len(logLevels))
	for i, lv := range logLevels {
		levels[i] = string(lv)
	}
	picker := logsTimeRangePicker(f, in.From, in.To, now)
	// 期間は Picker で選ぶ。検索し直しても今の期間を保つ
	keep := []components.KeptParamViewModel{{Name: "range", Value: f.RangeKey}}
	if f.fixed() {
		keep = []components.KeptParamViewModel{{Name: "since", Value: picker.Since}, {Name: "until", Value: picker.Until}}
	}
	logs := toLogListViewModel(out)
	logs.ResetURL = f.resetURL()
	vm := pages.LogsViewModel{
		TargetURL: h.targetURL, Logs: logs, Picker: picker,
		Search: components.SearchFormViewModel{
			Action: "/logs", Query: f.Query, Placeholder: "search text (e.g. a trace_id from Datadog)",
			Select: components.SearchSelectViewModel{Name: "level", All: "all levels", Options: levels, Selected: string(f.Level)}, Keep: keep,
		},
	}
	if !f.fixed() {
		vm.StreamURL = logsStreamURL + "?" + f.query().Encode()
	}
	if f.TimeErr != nil {
		vm.TimeError = f.TimeErr.Error()
	}
	if err := pages.Logs(vm).Render(ctx, rw); err != nil {
		slog.ErrorContext(ctx, "render logs", slog.Any("err", err))
	}
}

const logsStreamURL = "/logs/stream"

// GetLogsStreamHandler は今に合わせて動く期間の Logs を、refresh ごとに SSE で送る (一覧と、見出しの範囲)
type GetLogsStreamHandler struct {
	listLogsUsecase *usecase.ListLogsUsecase
	interval        time.Duration
}

func NewGetLogsStreamHandler(listLogsUsecase *usecase.ListLogsUsecase, interval time.Duration) *GetLogsStreamHandler {
	return &GetLogsStreamHandler{listLogsUsecase: listLogsUsecase, interval: interval}
}

func (h *GetLogsStreamHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := logsFilterOf(r)
	// 画面を開いている間ずっと書き続けるので、http.Server の WriteTimeout をこのリクエストだけ外す
	if err := http.NewResponseController(rw).SetWriteDeadline(time.Time{}); err != nil {
		slog.ErrorContext(ctx, "clear write deadline for logs stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	sse, err := turbo.NewStreamSSEWriter(rw)
	if err != nil {
		slog.ErrorContext(ctx, "open logs stream", slog.Any("err", err))
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		in := f.input(now)
		out, err := h.listLogsUsecase.Exec(ctx, in)
		if err != nil {
			slog.WarnContext(ctx, "list logs", slog.Any("err", err))
			return
		}
		logs := toLogListViewModel(out)
		logs.ResetURL = f.resetURL()
		rg := timeRangeOver(in.From, in.To, f.Span)
		if f.fixed() {
			rg = timeRange(in.From, in.To)
		}
		if err := sse.Write(ctx, partials.LogsStreamUpdate(logs, rg)); err != nil {
			return
		}
	}
}

func toLogListViewModel(out usecase.ListLogsUsecaseOutput) components.LogListViewModel {
	vm := components.LogListViewModel{Total: out.Total, Shown: len(out.Lines)}

	addLogTrend(&vm, out)

	sources := map[string]bool{}
	for _, l := range out.Lines {
		sources[l.Line.Source] = true
		openURL := ""
		if l.Replayable {
			openURL = fmt.Sprintf("/logs/%d/open", l.Line.ID)
		}
		vm.Lines = append(vm.Lines, components.LogRowViewModel{
			ID:         fmt.Sprintf("log-%d", l.Line.ID),
			OpenURL:    openURL,
			Time:       l.Line.At.Local().Format("15:04:05.000"),
			Level:      levelLabel(l.Line.Level),
			LevelClass: levelClass(l.Line.Level),
			Message:    l.Line.Message,
			Source:     l.Line.Source,
			Exact:      l.Line.HasTraceSeq,
		})
		row := &vm.Lines[len(vm.Lines)-1]
		row.Fields, row.Detail = logDetail(l.Line)
	}
	vm.ShowSource = len(sources) > 1
	return vm
}

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

// logRange は ?range= (Overview と同じ期間の名前) を読む
func logRange(r *http.Request) (string, time.Duration, bool) {
	return logRangeOf(r.URL.Query().Get("range"))
}

func logRangeOf(key string) (string, time.Duration, bool) {
	for _, rg := range overviewRanges {
		if rg.Key == key {
			return rg.Key, rg.Span, true
		}
	}
	return "", 0, false
}

// logsTimeRangePicker は Logs の期間を選ぶ部品。今に合わせて動く期間なら動いている印を付け、範囲は refresh ごとに SSE で書き換わる
func logsTimeRangePicker(f logsFilter, from, to, now time.Time) components.TimeRangePickerViewModel {
	vm := components.TimeRangePickerViewModel{Action: "/logs", TimeZone: timeZoneLabel(), RangeID: partials.LogsRangeID, Live: !f.fixed()}
	until := to
	if until.IsZero() {
		until = now
	}
	since := from
	if since.IsZero() || !since.Before(until) {
		since = until.Add(-time.Hour)
	}
	vm.Since, vm.Until = since.Local().Format(localTimeInputLayout), until.Local().Format(localTimeInputLayout)
	if f.fixed() {
		vm.Range = timeRange(since, until)
	} else {
		vm.Range = timeRangeOver(since, until, f.Span)
	}
	keep := f.query()
	keep.Del("range")
	for _, rg := range overviewRanges {
		q := maps.Clone(keep)
		q.Set("range", rg.Key)
		vm.Presets = append(vm.Presets, components.TimeRangePresetViewModel{Label: rg.Key, URL: "/logs?" + q.Encode(), Active: rg.Key == f.RangeKey})
	}
	vm.Keep = keptParams(keep)
	return vm
}

// logDetail は行を開いたときに見せるもの。JSON のオブジェクトなら、書かれた順のキーと値の表にする (入れ子は "a.b" のキーにする)。
// そうでなければ取り込んだままの行。本文と同じなら、見せるものがないので空
func logDetail(l model.LogLine) ([]components.LogFieldViewModel, string) {
	raw := strings.TrimSpace(l.Raw)
	if raw == "" || raw == l.Message {
		return nil, ""
	}
	if fields, ok := jsonFields(raw); ok {
		return fields, ""
	}
	return nil, raw
}

// jsonFields は JSON のオブジェクトを、書かれた順のキーと値にする。配列はそのまま JSON で、文字列は引用符を外して出す
func jsonFields(raw string) ([]components.LogFieldViewModel, bool) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var out []components.LogFieldViewModel
	if err := readJSONObject(dec, "", &out); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return out, true
}

func readJSONObject(dec *json.Decoder, prefix string, out *[]components.LogFieldViewModel) error {
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return errors.New("not a JSON object")
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		key := prefix + t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return err
		}
		if len(v) > 0 && v[0] == '{' {
			if err := readJSONObject(json.NewDecoder(bytes.NewReader(v)), key+".", out); err != nil {
				return err
			}
			continue
		}
		value := string(v)
		var str string
		if json.Unmarshal(v, &str) == nil {
			value = str
		}
		*out = append(*out, components.LogFieldViewModel{Key: key, Value: value})
	}
	_, err := dec.Token()
	return err
}

// logTrendLevels は件数の推移で積み上げるレベル。重いものを下にする
var logTrendLevels = append(append([]model.LogLevel{}, logLevels...), model.LogLevelUnknown)

// addLogTrend は件数の推移を、Overview・Issues と同じ積み上げの棒グラフ (同じ横軸・hover・拡大) にする
func addLogTrend(vm *components.LogListViewModel, out usecase.ListLogsUsecaseOutput) {
	f := chartFrame{step: out.Step}
	if len(out.Histogram) > 0 {
		f.span = out.Histogram[len(out.Histogram)-1].End.Sub(out.Histogram[0].Start)
	}
	for _, b := range out.Histogram {
		f.steps = append(f.steps, b.Start)
	}
	var series []chartSeries
	for _, lv := range logTrendLevels {
		label := levelLabel(lv)
		if lv == model.LogLevelUnknown {
			label = "no level"
		}
		cs := chartSeries{label: label, class: "series-level-" + levelClass(lv)}
		for _, b := range out.Histogram {
			cs.values = append(cs.values, usecase.MetricSample{Value: float64(b.Counts[lv]), OK: true})
		}
		series = append(series, cs)
	}
	vm.Steps = f.stepsJSON()
	vm.Chart = f.stackedBarChart(series)
}
