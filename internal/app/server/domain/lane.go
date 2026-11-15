package domain

import (
	"github.com/kakkky/gorviz/internal/app/api/model"
	"slices"
	"time"
)

func Utilization(l model.ProcLane, window time.Duration) float64 {
	if window <= 0 {
		return 0
	}
	var busy time.Duration
	for _, s := range l.Spans {
		busy += s.End - s.Start
	}
	return float64(busy) / float64(window)
}

// LastSpan は観測窓の最後に P の上にいた G。窓の終わりまで走っていたかは End で判断する。
func LastSpan(l model.ProcLane) (model.SchedSpan, bool) {
	if len(l.Spans) == 0 {
		return model.SchedSpan{}, false
	}
	return l.Spans[len(l.Spans)-1], true
}

// LaneBucket は観測窓を等分した 1 区画。区画の中で一番長く走った G を代表として持つ。
type LaneBucket struct {
	Goroutine model.GoroutineID
	Thread    model.ThreadID
	Busy      float64
}

// Buckets は描画用に窓を n 等分する。1 秒の trace には数千の span が入りうるので、
// GUI / CLI ともに span をそのまま描かず、幅の決まった区画にまとめてから描く。
func Buckets(l model.ProcLane, window time.Duration, n int) []LaneBucket {
	buckets := make([]LaneBucket, n)
	if window <= 0 || n <= 0 {
		return buckets
	}
	width := window / time.Duration(n)
	longest := make([]time.Duration, n)
	for _, s := range l.Spans {
		first := min(int(s.Start/width), n-1)
		last := min(int(s.End/width), n-1)
		for i := first; i <= last; i++ {
			bStart := time.Duration(i) * width
			overlap := min(s.End, bStart+width) - max(s.Start, bStart)
			if overlap <= 0 {
				continue
			}
			buckets[i].Busy += float64(overlap) / float64(width)
			if overlap > longest[i] {
				longest[i] = overlap
				buckets[i].Goroutine = s.Goroutine
				buckets[i].Thread = s.Thread
			}
		}
	}
	return buckets
}

func SortedProcs(t model.SchedulerTimeline) []model.ProcLane {
	procs := slices.Clone(t.Procs)
	slices.SortFunc(procs, func(a, b model.ProcLane) int { return int(a.Proc - b.Proc) })
	return procs
}
