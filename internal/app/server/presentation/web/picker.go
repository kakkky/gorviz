package web

import (
	"net/url"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
)

// preset はよく使う期間 1 つ。from は今からの相対 (-1h)
type preset struct {
	label string
	from  string
}

// picker は期間を選ぶ部品。keep は期間を変えても残す query (絞り込み)。def は from を渡さなかったときの期間
func picker(action string, keep url.Values, r timeRange, from, to time.Time, presets []preset, def string) components.TimeRangePickerViewModel {
	vm := components.TimeRangePickerViewModel{Live: r.Live(), Action: action, From: rfc(from), To: rfc(to), Range: rangeView(from, to)}
	for k, vs := range keep {
		for _, v := range vs {
			vm.Keep = append(vm.Keep, components.KeptParamViewModel{Name: k, Value: v})
		}
	}
	current := r.FromRaw
	if current == "" {
		current = def
	}
	for _, p := range presets {
		q := url.Values{}
		for k, vs := range keep {
			q[k] = vs
		}
		q.Set("from", p.from)
		active := vm.Live && current == p.from
		vm.Presets = append(vm.Presets, components.PresetViewModel{Label: p.label, URL: withQuery(action, q), Active: active})
	}
	return vm
}

// rangeView は見出しに出す期間。1 分より短い区間 (再生) はミリ秒まで出す
func rangeView(from, to time.Time) components.TimeRangeViewModel {
	layout := "01/02 15:04:05"
	if to.Sub(from) < time.Minute {
		layout = "01/02 15:04:05.000"
	}
	return components.TimeRangeViewModel{From: from.Local().Format(layout), To: to.Local().Format(layout), Length: lengthText(to.Sub(from))}
}

// lengthText は期間の長さを 1h・15m・500ms のように短く書く
func lengthText(d time.Duration) string {
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return itoa(int(d/(24*time.Hour))) + "d"
	case d >= time.Hour && d%time.Hour == 0:
		return itoa(int(d/time.Hour)) + "h"
	case d >= time.Minute && d%time.Minute == 0:
		return itoa(int(d/time.Minute)) + "m"
	}
	return duration(d)
}
