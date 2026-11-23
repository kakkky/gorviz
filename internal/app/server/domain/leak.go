package domain

import (
	"cmp"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// LeakMatch はリーク判定の 1 つのスタックを、同じ時点のスナップショットと突き合わせた結果。同じスタックの Of 個のうち Count 個がリーク
type LeakMatch struct {
	Stack      []observation.Frame
	Count      int
	Of         int
	Goroutines []observation.GoroutineID
	Certainty  Certainty
	CreatedBy  observation.Frame
	Current    observation.Frame
	WaitReason string
}

// MatchLeaks はリーク判定のスタックごとに、どの goroutine がリークしたかを決める。返す列は lc.Leaks と同じ順。
// 同じスタックの goroutine がちょうど件数だけあれば全部 (FACT)、多ければ待っている時間が長い順に件数分 (INFERENCE)
func MatchLeaks(lc observation.GoroutineLeakCheck, snapshot observation.GoroutineSnapshot) []LeakMatch {
	byStack := map[string][]observation.Goroutine{}
	for _, g := range snapshot.Goroutines {
		k := stackKey(g.Stack)
		byStack[k] = append(byStack[k], g)
	}
	out := make([]LeakMatch, 0, len(lc.Leaks))
	for _, l := range lc.Leaks {
		same := slices.Clone(byStack[stackKey(l.Stack)])
		m := LeakMatch{Stack: l.Stack, Count: l.Count, Of: len(same), Current: observation.CurrentFrame(l.Stack)}
		slices.SortStableFunc(same, func(a, b observation.Goroutine) int {
			return cmp.Or(cmp.Compare(b.Waiting, a.Waiting), cmp.Compare(a.ID, b.ID))
		})
		m.Certainty = CertaintyInference
		if len(same) == l.Count {
			m.Certainty = CertaintyFact
		}
		for _, g := range same[:min(l.Count, len(same))] {
			m.Goroutines = append(m.Goroutines, g.ID)
		}
		if len(same) > 0 {
			m.CreatedBy, m.WaitReason = same[0].CreatedBy, same[0].WaitReason
		}
		out = append(out, m)
	}
	return out
}

// LeakOf は g がリークしたと決めた突き合わせ
func LeakOf(matches []LeakMatch, g observation.GoroutineID) (LeakMatch, bool) {
	for _, m := range matches {
		if slices.Contains(m.Goroutines, g) {
			return m, true
		}
	}
	return LeakMatch{}, false
}

// NearestSnapshot は at に一番近いスナップショット
func NearestSnapshot(snapshots []observation.GoroutineSnapshot, at time.Time) (observation.GoroutineSnapshot, bool) {
	var (
		best observation.GoroutineSnapshot
		diff time.Duration
		ok   bool
	)
	for _, s := range snapshots {
		d := s.At.Sub(at).Abs()
		if !ok || d < diff {
			best, diff, ok = s, d, true
		}
	}
	return best, ok
}
