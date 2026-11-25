package web

import (
	"net/url"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
)

// fromSession は、セッション全体を見る from の値
const fromSession = "session"

// timeRange は画面の期間。from が "session" か "-10s" のように今からの相対なら、新しく届いた分に合わせて動く (Live)
type timeRange struct {
	FromRaw string
	ToRaw   string
	From    time.Duration
	To      time.Duration
}

func (r timeRange) Live() bool {
	return r.FromRaw == fromSession || strings.HasPrefix(r.FromRaw, "-")
}

func (r timeRange) query() url.Values {
	q := url.Values{}
	q.Set("from", r.FromRaw)
	if r.ToRaw != "" {
		q.Set("to", r.ToRaw)
	}
	return q
}

// parseRange は query の from・to を、セッションの経過時間 last に当てはめる。from がなければ def
func parseRange(q url.Values, last time.Duration, def string) (timeRange, error) {
	r := timeRange{FromRaw: q.Get("from"), ToRaw: q.Get("to")}
	if r.FromRaw == "" {
		r.FromRaw, r.ToRaw = def, ""
	}
	switch {
	case r.FromRaw == fromSession:
		r.From, r.To = 0, last
	case strings.HasPrefix(r.FromRaw, "-"):
		d, err := time.ParseDuration(strings.TrimPrefix(r.FromRaw, "-"))
		if err != nil {
			return timeRange{}, domain.ErrInvalidArgument.With("from must be session, a length back from now like -30s, or a time since the start of the session like 12.5s")
		}
		r.From, r.To = max(0, last-d), last
	default:
		from, err := time.ParseDuration(r.FromRaw)
		if err != nil {
			return timeRange{}, domain.ErrInvalidArgument.With("from must be a time since the start of the session, like 12.5s")
		}
		to, err := time.ParseDuration(r.ToRaw)
		if err != nil {
			return timeRange{}, domain.ErrInvalidArgument.With("to must be a time since the start of the session, like 14s")
		}
		if to <= from {
			return timeRange{}, domain.ErrInvalidArgument.With("the end of the range must be after its start")
		}
		r.From, r.To = from, to
	}
	return r, nil
}

type preset struct {
	label string
	from  string
	to    string
}

// presetsOf はよく使う期間。セッション全体・直近と、診断の phase それぞれ
func presetsOf(phases []domain.PhaseRange) []preset {
	out := []preset{{label: "whole session", from: fromSession}, {label: "last 10s", from: "-10s"}, {label: "last 1m", from: "-1m"}}
	for _, p := range phases {
		out = append(out, preset{label: p.Name, from: at(p.From), to: at(p.To)})
	}
	return out
}

// picker は期間を選ぶ部品。keep は期間を変えても残す query (絞り込み)
func picker(action string, keep url.Values, r timeRange, phases []domain.PhaseRange) components.TimeRangePickerViewModel {
	vm := components.TimeRangePickerViewModel{Live: r.Live(), Action: action, From: at(r.From), To: at(r.To), Range: rangeView(r.From, r.To)}
	for k, vs := range keep {
		for _, v := range vs {
			vm.Keep = append(vm.Keep, components.KeptParamViewModel{Name: k, Value: v})
		}
	}
	for _, p := range presetsOf(phases) {
		q := url.Values{}
		for k, vs := range keep {
			q[k] = vs
		}
		q.Set("from", p.from)
		if p.to != "" {
			q.Set("to", p.to)
		}
		active := r.FromRaw == p.from && r.ToRaw == p.to
		vm.Presets = append(vm.Presets, components.PresetViewModel{Label: p.label, URL: action + "?" + q.Encode(), Active: active})
	}
	return vm
}

func rangeView(from, to time.Duration) components.TimeRangeViewModel {
	return components.TimeRangeViewModel{From: at(from), To: at(to), Length: duration(to - from)}
}

// phasesOf はセッションの出来事から、phase (と shutdown) が流れていた期間を組む
func phasesOf(events []domain.SessionEvent, last time.Duration) []domain.PhaseRange {
	var out []domain.PhaseRange
	for i, e := range events {
		if e.Kind != domain.EventPhase && e.Kind != domain.EventShutdown {
			continue
		}
		end := last
		if i+1 < len(events) {
			end = events[i+1].At
		}
		name := e.Phase
		if e.Kind == domain.EventShutdown {
			name = domain.PhaseShutdown
		}
		out = append(out, domain.PhaseRange{Name: name, From: e.At, To: end})
	}
	return out
}

// eventText はセッションの出来事 1 つを、グラフの印の説明に書く
func eventText(e domain.SessionEvent) string {
	s := at(e.At) + " " + string(e.Kind)
	if e.Phase != "" {
		s += " " + e.Phase
	}
	if e.Note != "" {
		s += " (" + e.Note + ")"
	}
	return s
}
