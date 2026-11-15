package domain

import "github.com/kakkky/gorviz/internal/app/api/model"

// EnrichFromDump は trace だけでは分からないことを goroutine dump で補う:
//   - 入口の関数: trace は窓の中で生まれた G の分しか持たないので、dump にいる G は dump の値で上書きする (trace のスタックは深さで切れることがある)
//   - 待機理由と今いる関数: 窓より前から待機していた G は trace に理由もスタックも載らない
//
// dump は trace と別の時点で取るので、待機理由と今いる関数は、dump の時点でも待機していた G にだけ使う。
func EnrichFromDump(t *model.SchedulerTimeline, gs []model.Goroutine) {
	if t.Entries == nil {
		t.Entries = map[model.GoroutineID]string{}
	}
	waiting := make(map[model.GoroutineID]model.Goroutine, len(gs))
	for _, g := range gs {
		if g.EntryFunction != "" {
			t.Entries[g.ID] = g.EntryFunction
		}
		if g.StateCategory() == model.GoroutineWaiting {
			waiting[g.ID] = g
		}
	}
	for i := range t.Events {
		e := &t.Events[i]
		if e.Kind != model.SchedEventGoroutine || e.Place.Kind != model.PlaceWaiting {
			continue
		}
		g, ok := waiting[e.Goroutine]
		if !ok {
			continue
		}
		if e.Place.Reason == model.UnknownWaitReason {
			e.Place.Reason = g.State
		}
		if e.Function == "" {
			e.Function = g.CurrentFunction
		}
	}
}
