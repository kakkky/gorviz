package domain

import (
	"github.com/kakkky/gorviz/internal/app/protocol/model"
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

func SortedProcs(t model.SchedulerTimeline) []model.ProcLane {
	procs := slices.Clone(t.Procs)
	slices.SortFunc(procs, func(a, b model.ProcLane) int { return int(a.Proc - b.Proc) })
	return procs
}
