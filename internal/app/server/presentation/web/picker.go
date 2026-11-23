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
	vm := components.TimeRangePickerViewModel{Live: r.Live(), Action: action, From: rfc(from), To: rfc(to), Label: span(from, to)}
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
		if active {
			vm.Label = p.label
		}
		vm.Presets = append(vm.Presets, components.PresetViewModel{Label: p.label, URL: withQuery(action, q), Active: active})
	}
	return vm
}
