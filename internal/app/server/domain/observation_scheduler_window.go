package domain

import (
	"sort"
	"time"
)

// maxWindowChanges は 1 つの窓で送る状態の変化の上限。ブラウザが重くなりすぎないようにする
const maxWindowChanges = 300000

// ProcSpan は P の上で goroutine が走っていた区間
type ProcSpan struct {
	G     GoroutineID
	Start time.Duration
	End   time.Duration
}

// SchedulerWindow はスケジューラの動きを再生する期間の材料。時刻は窓の始まりからの経過時間で、
// Changes の先頭には、窓の始まりにいた goroutine の状態を時刻 0 として置く
type SchedulerWindow struct {
	From        time.Duration
	To          time.Duration
	Procs       []int16
	Changes     []StateChange
	ProcChanges []ProcChange
	Lanes       map[int16][]ProcSpan
	STW         []TimeRange
	GCMark      []TimeRange
	Entries     map[GoroutineID]string
	Gorviz      map[GoroutineID]bool
	Functions   map[StackID]string
	Reasons     map[uint16]string
	Truncated   bool
	Ended       bool
	Live        bool
}

func NewSchedulerWindow(ix *ObservationSet, from, to time.Duration) SchedulerWindow {
	to = min(to, ix.Last())
	from = min(from, to)
	w := SchedulerWindow{From: from, To: to, Lanes: map[int16][]ProcSpan{}, Entries: map[GoroutineID]string{}, Gorviz: map[GoroutineID]bool{}, Functions: map[StackID]string{}, Reasons: map[uint16]string{}}
	procs := map[int16]bool{}
	note := func(c StateChange) StateChange {
		c.At -= from
		if c.At < 0 {
			c.At = 0
		}
		if _, ok := w.Functions[c.Stack]; !ok {
			w.Functions[c.Stack] = CurrentFrame(ix.Stack(c.Stack)).Function
		}
		w.Reasons[c.Reason] = ix.Reason(c.Reason)
		if c.Proc >= 0 {
			procs[c.Proc] = true
		}
		if g, ok := ix.Goroutine(c.G); ok {
			w.Entries[c.G] = g.Entry
			if g.Gorviz {
				w.Gorviz[c.G] = true
			}
		}
		return c
	}
	running := map[GoroutineID]StateChange{}
	for _, id := range ix.GoroutineIDs() {
		g, _ := ix.Goroutine(id)
		c, ok := ix.StateAt(g, from)
		if !ok || c.Place == PlaceDead {
			continue
		}
		c = note(c)
		w.Changes = append(w.Changes, c)
		if c.Place == PlaceRunning {
			running[c.G] = c
		}
	}
	lo, hi := ix.ChangesBetween(from, to)
	if hi-lo > maxWindowChanges {
		hi = lo + maxWindowChanges
		w.Truncated = true
	}
	for i := lo; i < hi; i++ {
		c := note(ix.ChangeAt(i))
		if prev, ok := running[c.G]; ok && c.Place != PlaceRunning {
			w.Lanes[prev.Proc] = append(w.Lanes[prev.Proc], ProcSpan{G: c.G, Start: prev.At, End: c.At})
			delete(running, c.G)
		}
		if c.Place == PlaceRunning && c.Proc >= 0 {
			running[c.G] = c
		}
		w.Changes = append(w.Changes, c)
	}
	for g, c := range running {
		w.Lanes[c.Proc] = append(w.Lanes[c.Proc], ProcSpan{G: g, Start: c.At, End: to - from})
	}
	for p := range w.Lanes {
		sort.Slice(w.Lanes[p], func(i, j int) bool { return w.Lanes[p][i].Start < w.Lanes[p][j].Start })
	}
	before, within := ix.ProcChanges(from, to)
	for _, p := range before {
		p.At = 0
		procs[p.Proc] = true
		w.ProcChanges = append(w.ProcChanges, p)
	}
	for _, p := range within {
		p.At -= from
		procs[p.Proc] = true
		w.ProcChanges = append(w.ProcChanges, p)
	}
	for p := int16(0); int(p) < ix.Procs(); p++ {
		procs[p] = true
	}
	for p := range procs {
		w.Procs = append(w.Procs, p)
	}
	sort.Slice(w.Procs, func(i, j int) bool { return w.Procs[i] < w.Procs[j] })
	for _, r := range ix.STW(from, to) {
		r.Start, r.End = max(0, r.Start-from), min(to, r.End)-from
		w.STW = append(w.STW, r)
	}
	for _, r := range ix.GCMark(from, to) {
		r.Start, r.End = max(0, r.Start-from), min(to, r.End)-from
		w.GCMark = append(w.GCMark, r)
	}
	w.Ended, _ = ix.Ended()
	return w
}
