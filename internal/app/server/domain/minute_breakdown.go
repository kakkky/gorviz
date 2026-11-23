package domain

import (
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

const BreakdownStep = time.Minute

// MinuteBreakdown は 1 つの goroutine の 1 分ごとの状態別の時間内訳。execution trace が消えたあとの診断に使う
type MinuteBreakdown struct {
	Minute    time.Time               `json:"minute"`
	Goroutine observation.GoroutineID `json:"goroutine"`
	Entry     string                  `json:"entry"`
	Breakdown StateBreakdown          `json:"breakdown"`
	Main      MainCause               `json:"main"`
	Stops     map[MainCause]StopPlace `json:"stops,omitempty"`
}

// BuildMinuteBreakdowns は 1 分 [minute, minute+1m) をつないだチャンクから、その 1 分に動いた goroutine ごとの内訳を作る。
// 1 分ずっと同じ待ちにいた goroutine は書かない。数が多く、スナップショットから同じことが分かるため
func BuildMinuteBreakdowns(joined observation.TraceChunk, minute time.Time) []MinuteBreakdown {
	from, to := joined.Offset(minute), joined.Offset(minute.Add(BreakdownStep))
	var out []MinuteBreakdown
	for _, g := range Goroutines(joined, 0, joined.Duration) {
		if !moved(joined, g, from, to) {
			continue
		}
		b := Breakdown(joined, g, from, to)
		main, ok := b.Main()
		if !ok {
			continue
		}
		mb := MinuteBreakdown{Minute: minute, Goroutine: g, Entry: joined.Entries[g], Breakdown: b, Main: main, Stops: map[MainCause]StopPlace{}}
		for c := range b.Durations {
			if c == CauseRunning {
				continue
			}
			if s, ok := LongestStop(joined, g, c, from, to); ok {
				mb.Stops[c] = s
			}
		}
		out = append(out, mb)
	}
	return out
}

func moved(c observation.TraceChunk, g observation.GoroutineID, from, to time.Duration) bool {
	n := 0
	for _, s := range Stretches(c, g) {
		if s.End <= from || s.Start >= to {
			continue
		}
		n++
		if n > 1 || s.Cause == CauseRunning || s.Cause == CauseRunnable {
			return true
		}
	}
	return false
}

// CompleteSteps は after より後に始まり、材料がそろった step 刻みの区切りの始まり。最大 limit 個
func CompleteSteps(after time.Time, first time.Time, step time.Duration, a Arrival, limit int) []time.Time {
	if a.Latest.IsZero() {
		return nil
	}
	start := first.UTC().Truncate(step)
	if !after.IsZero() {
		start = after.Add(step)
	}
	var out []time.Time
	for t := start; len(out) < limit && a.MaterialReady(t.Add(step)) && !t.After(a.Latest); t = t.Add(step) {
		out = append(out, t)
	}
	return out
}
