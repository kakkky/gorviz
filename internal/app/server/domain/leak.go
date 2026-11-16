package domain

import (
	"cmp"
	"github.com/kakkky/gorviz/internal/app/api/model"
	"slices"
	"time"
)

// GoroutineSample は 1 回の観測で、生成元ごとに何個の G が待機していたか。
type GoroutineSample struct {
	At              time.Time
	WaitingByOrigin map[model.GoroutineOrigin]int
}

func NewGoroutineSample(at time.Time, gs []model.Goroutine) GoroutineSample {
	waiting := map[model.GoroutineOrigin]int{}
	for _, g := range gs {
		if g.StateCategory() == model.GoroutineWaiting {
			waiting[g.Origin()]++
		}
	}
	return GoroutineSample{At: at, WaitingByOrigin: waiting}
}

type Leak struct {
	Origin      model.GoroutineOrigin
	Count       int
	Growth      int
	LongestWait time.Duration
	// Trend は dump ごとの、この生成元の待機中の G の数 (古い順)
	Trend []int
	// Certainty は、runtime の goroutineleak プロファイル (GC で到達不能と判定された G) に載ったものなら FACT。
	// 件数の推移から推測したものは INFERENCE
	Certainty Certainty
}

type LeakDetectionPolicy struct {
	MinSamples int
	MinGrowth  int
}

// DetectLeaks は 2 つの根拠を合わせる:
//   - Leaked の立った G: runtime が到達不能と判定した G。誤検知がないので、件数の推移を見ずに FACT とする
//   - samples: dump のたびに生成元ごとの待機数を並べ、一度も減らずに MinGrowth 以上増えた生成元を INFERENCE とする
func DetectLeaks(samples []GoroutineSample, current []model.Goroutine, policy LeakDetectionPolicy) []Leak {
	longestWait := map[model.GoroutineOrigin]time.Duration{}
	for _, g := range current {
		longestWait[g.Origin()] = max(longestWait[g.Origin()], g.Wait)
	}

	suspects := map[model.GoroutineOrigin]*Leak{}
	for _, g := range current {
		if !g.Leaked {
			continue
		}
		s, ok := suspects[g.Origin()]
		if !ok {
			s = &Leak{Origin: g.Origin(), Certainty: CertaintyFact}
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
			suspects[origin] = &Leak{
				Certainty:   CertaintyInference,
				Origin:      origin,
				Count:       count,
				Growth:      growth,
				LongestWait: longestWait[origin],
			}
		}
	}

	out := make([]Leak, 0, len(suspects))
	for _, s := range suspects {
		s.Trend = waitingTrend(samples, s.Origin)
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b Leak) int {
		if a.Certainty != b.Certainty {
			if a.Certainty == CertaintyFact {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(b.Growth, a.Growth), cmp.Compare(b.Count, a.Count), cmp.Compare(a.Origin, b.Origin))
	})
	return out
}

func monotonicGrowth(samples []GoroutineSample, origin model.GoroutineOrigin) (int, bool) {
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

func waitingTrend(samples []GoroutineSample, origin model.GoroutineOrigin) []int {
	if len(samples) == 0 {
		return nil
	}
	trend := make([]int, len(samples))
	for i, s := range samples {
		trend[i] = s.WaitingByOrigin[origin]
	}
	return trend
}
