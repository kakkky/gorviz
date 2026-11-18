package handler

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
)

// 時刻の入力欄 (<input type="datetime-local">) の値の形。秒は省かれることがある
var localTimeLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04"}

const localTimeInputLayout = "2006-01-02T15:04:05"

// localTimeInputLayoutMs は ms まで選ぶ入力欄 (再生の区間) の値の形
const localTimeInputLayoutMs = "2006-01-02T15:04:05.000"

// localTimeParam は時刻の入力欄の値を、server のタイムゾーンの時刻として読む。画面の時刻もすべて server のタイムゾーンで出している
func localTimeParam(r *http.Request, key string) (time.Time, bool, error) {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return time.Time{}, false, nil
	}
	for _, layout := range localTimeLayouts {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, true, nil
		}
	}
	return time.Time{}, false, errors.New("cannot read the time " + strconv.Quote(v))
}

// timeZoneLabel は入力欄の横に出す、server のタイムゾーンの名前
func timeZoneLabel() string {
	name, _ := time.Now().Zone()
	return name
}

// timeRange は画面に出す時刻の範囲。両端に日付を付け、1 分より短い範囲は ms まで出す
func timeRange(from, to time.Time) components.TimeRangeViewModel {
	layout := "01/02 15:04:05"
	if to.Sub(from) < time.Minute {
		layout = "01/02 15:04:05.000"
	}
	return components.TimeRangeViewModel{From: from.Local().Format(layout), To: to.Local().Format(layout), Length: formatLength(to.Sub(from))}
}

// timeRangePicker は Overview・Issues・Issue の画面の期間を選ぶ部品。拡大していなければ今に合わせて動く期間 (refresh ごとに rangeID の所を SSE で書き換える)、
// 拡大していればその範囲。presetURL は期間ごとの先、keep は入力欄で期間を選び直すときにも保つ絞り込み
func timeRangePicker(w overviewWindow, now time.Time, rangeID, action string, keep url.Values, presetURL func(key string) string) components.TimeRangePickerViewModel {
	end, span := w.bounds(now)
	vm := components.TimeRangePickerViewModel{
		Range: timeRangeOver(end.Add(-span), end, span), RangeID: rangeID, Live: !w.zoomed(),
		Action: action, TimeZone: timeZoneLabel(), Since: end.Add(-span).Local().Format(localTimeInputLayout), Until: end.Local().Format(localTimeInputLayout),
	}
	for _, rg := range overviewRanges {
		vm.Presets = append(vm.Presets, components.TimeRangePresetViewModel{Label: rg.Key, URL: presetURL(rg.Key), Active: rg.Key == w.Key && !w.zoomed()})
	}
	vm.Keep = keptParams(keep)
	return vm
}

func keptParams(q url.Values) []components.KeptParamViewModel {
	var out []components.KeptParamViewModel
	for _, name := range slices.Sorted(maps.Keys(q)) {
		if v := q.Get(name); v != "" {
			out = append(out, components.KeptParamViewModel{Name: name, Value: v})
		}
	}
	return out
}

// timeRangeOver は選んだ期間 (span) の範囲。端は区切りにそろえてあり、長さは選んだ期間と少しずれるので、よく使う期間なら長さにはその期間を書く。
// 拡大した期間 (よく使う期間でない) なら、端のあいだの長さを書く
func timeRangeOver(from, to time.Time, span time.Duration) components.TimeRangeViewModel {
	vm := timeRange(from, to)
	if slices.ContainsFunc(overviewRanges, func(rg struct {
		Key  string
		Span time.Duration
	}) bool {
		return rg.Span == span
	}) {
		vm.Length = formatLength(span)
	}
	return vm
}

// formatLength は範囲の長さを、桁に合わせて短く書く (282ms・1.62s・15m30s・6h30m・30d)。0 の下の桁は書かない
func formatLength(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(time.Millisecond).String()
	case d >= 48*time.Hour || d%(24*time.Hour) == 0:
		return fmt.Sprintf("%.0fd", d.Hours()/24)
	case d >= time.Hour:
		d = d.Round(time.Minute)
		return unitsOf(int64(d/time.Hour), "h", int64(d%time.Hour/time.Minute), "m")
	default:
		d = d.Round(time.Second)
		return unitsOf(int64(d/time.Minute), "m", int64(d%time.Minute/time.Second), "s")
	}
}

func unitsOf(major int64, majorUnit string, minor int64, minorUnit string) string {
	if minor == 0 {
		return fmt.Sprintf("%d%s", major, majorUnit)
	}
	return fmt.Sprintf("%d%s%d%s", major, majorUnit, minor, minorUnit)
}

// spanOf は overviewWindow.bounds の戻り値から期間の長さだけを取る
func spanOf(_ time.Time, span time.Duration) time.Duration {
	return span
}
