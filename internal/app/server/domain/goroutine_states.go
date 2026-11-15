package domain

import "github.com/kakkky/gorviz/internal/app/api/model"

type GoroutineStateCounts map[model.GoroutineStateCategory]int

func CountGoroutineStates(gs []model.Goroutine) GoroutineStateCounts {
	counts := GoroutineStateCounts{}
	for _, g := range gs {
		counts[g.StateCategory()]++
	}
	return counts
}
