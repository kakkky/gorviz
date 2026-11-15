package domain

import (
	"cmp"
	"slices"
	"time"
)

// GoroutineSample は 1 回の観測で、生成元ごとに何個の G が待機していたか。
type GoroutineSample struct {
	At              time.Time
	WaitingByOrigin map[GoroutineOrigin]int
}

func NewGoroutineSample(at time.Time, gs []Goroutine) GoroutineSample {
	waiting := map[GoroutineOrigin]int{}
	for _, g := range gs {
		if g.StateCategory() == GoroutineWaiting {
			waiting[g.Origin()]++
		}
	}
	return GoroutineSample{At: at, WaitingByOrigin: waiting}
}

type LeakSuspect struct {
	Origin      GoroutineOrigin
	Count       int
	Growth      int
	LongestWait time.Duration
	// Confirmed は runtime の goroutineleak プロファイル (GC で到達不能と判定された G) に載ったもの。
	// それ以外は件数の推移から推測した疑い。
	Confirmed bool
}

type LeakDetectionPolicy struct {
	MinSamples int
	MinGrowth  int
}

// DetectLeakSuspects は 2 つの根拠を合わせる:
//   - Leaked の立った G: runtime が到達不能と判定した G。誤検知がないので、件数の推移を見ずに確定として扱う
//   - samples: 観測のたびに生成元ごとの待機数を並べ、一度も減らずに MinGrowth 以上増えた生成元を疑いとする
func DetectLeakSuspects(samples []GoroutineSample, current []Goroutine, policy LeakDetectionPolicy) []LeakSuspect {
	longestWait := map[GoroutineOrigin]time.Duration{}
	for _, g := range current {
		longestWait[g.Origin()] = max(longestWait[g.Origin()], g.Wait)
	}

	suspects := map[GoroutineOrigin]*LeakSuspect{}
	for _, g := range current {
		if !g.Leaked {
			continue
		}
		s, ok := suspects[g.Origin()]
		if !ok {
			s = &LeakSuspect{Origin: g.Origin(), Confirmed: true}
			suspects[g.Origin()] = s
		}
		s.Count++
		s.LongestWait = max(s.LongestWait, g.Wait)
	}

	if len(samples) >= policy.MinSamples && len(samples) > 0 {
		latest := samples[len(samples)-1]
		for origin, count := range latest.WaitingByOrigin {
			growth, ok := monotonicGrowth(samples, origin)
			if !ok || growth < policy.MinGrowth {
				continue
			}
			if s, exists := suspects[origin]; exists {
				s.Growth = growth
				continue
			}
			suspects[origin] = &LeakSuspect{
				Origin:      origin,
				Count:       count,
				Growth:      growth,
				LongestWait: longestWait[origin],
			}
		}
	}

	out := make([]LeakSuspect, 0, len(suspects))
	for _, s := range suspects {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b LeakSuspect) int {
		if a.Confirmed != b.Confirmed {
			if a.Confirmed {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(b.Growth, a.Growth), cmp.Compare(b.Count, a.Count), cmp.Compare(a.Origin, b.Origin))
	})
	return out
}

func monotonicGrowth(samples []GoroutineSample, origin GoroutineOrigin) (int, bool) {
	prev := samples[0].WaitingByOrigin[origin]
	for _, s := range samples[1:] {
		cur := s.WaitingByOrigin[origin]
		if cur < prev {
			return 0, false
		}
		prev = cur
	}
	return prev - samples[0].WaitingByOrigin[origin], true
}
