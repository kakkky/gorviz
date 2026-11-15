package domain

import (
	"slices"
	"strings"
	"time"
)

// PrimaryCause は Goroutine の診断の主因。状態別の時間内訳のうち一番長かったもので、内訳は全部の時間をもれなくどれかに分ける。
type PrimaryCause string

const (
	CauseRunning PrimaryCause = "RUNNING"
	CauseCPUWait PrimaryCause = "CPU_WAIT"
	CauseBlocked PrimaryCause = "BLOCKED"
	CauseIOWait  PrimaryCause = "IO_WAIT"
	// CauseCGO は cgo で C のコードを実行していた時間。trace ではシステムコールと見分けられないので、今は判定しない (IO_WAIT に入る)
	CauseCGO   PrimaryCause = "CGO"
	CauseSleep PrimaryCause = "SLEEP"
	CauseGC    PrimaryCause = "GC"
)

// PrimaryCauses は表示の順
var PrimaryCauses = []PrimaryCause{CauseRunning, CauseCPUWait, CauseBlocked, CauseIOWait, CauseCGO, CauseSleep, CauseGC}

// CauseOfWaitReason は待機理由を主因に読み替える。trace の理由 ("network" など) と dump の理由 ("IO wait" など) のどちらも受け付ける
func CauseOfWaitReason(reason string) PrimaryCause {
	switch {
	case reason == "network" || reason == "IO wait":
		return CauseIOWait
	case reason == "sleep":
		return CauseSleep
	case strings.Contains(reason, "GC"):
		return CauseGC
	default:
		return CauseBlocked
	}
}

func causeOfPlace(p GoroutinePlace) (PrimaryCause, bool) {
	switch p.Kind {
	case PlaceRunning:
		return CauseRunning, true
	case PlaceLocalQueue, PlaceGlobalQueue:
		return CauseCPUWait, true
	case PlaceWaiting:
		return CauseOfWaitReason(p.Reason), true
	case PlaceSyscall:
		return CauseIOWait, true
	}
	return "", false
}

// CauseOfGoroutine は trace がないときに、dump の 1 時点の状態から主因を推し量る
func CauseOfGoroutine(g Goroutine) PrimaryCause {
	switch g.StateCategory() {
	case GoroutineRunning:
		return CauseRunning
	case GoroutineRunnable:
		return CauseCPUWait
	case GoroutineSyscall:
		return CauseIOWait
	default:
		return CauseOfWaitReason(g.State)
	}
}

// StateBreakdown は 1 つの G の、ある区間での状態別の時間内訳。
// Unrecorded は区間のうち trace に記録がなかった時間 (録画の区切りのすき間や、G がまだ / もういなかった時間)
type StateBreakdown struct {
	Durations  map[PrimaryCause]time.Duration
	Unrecorded time.Duration
}

func NewStateBreakdown() StateBreakdown {
	return StateBreakdown{Durations: map[PrimaryCause]time.Duration{}}
}

func (b StateBreakdown) Recorded() time.Duration {
	var total time.Duration
	for _, d := range b.Durations {
		total += d
	}
	return total
}

// Primary は一番長かった主因。記録が 1 つもなければ false
func (b StateBreakdown) Primary() (PrimaryCause, bool) {
	var (
		best    PrimaryCause
		longest time.Duration
	)
	for _, c := range PrimaryCauses {
		if d := b.Durations[c]; d > longest {
			best, longest = c, d
		}
	}
	return best, longest > 0
}

func (b StateBreakdown) Add(other StateBreakdown) {
	for c, d := range other.Durations {
		b.Durations[c] += d
	}
}

type causeSegment struct {
	cause      PrimaryCause
	start, end time.Duration
}

// Breakdown は g の [from, to] の時間を主因ごとに分ける。区間は Timeline 上の時刻。
// GC の区間 (STW と、その G の mark assist) に重なる時間は、元の状態によらず GC に数える
func (t SchedulerTimeline) Breakdown(g GoroutineID, from, to time.Duration) StateBreakdown {
	out := NewStateBreakdown()
	from, to = max(from, 0), min(to, t.Window)
	if to <= from {
		return out
	}

	var (
		segments []causeSegment
		cur      PrimaryCause
		curStart time.Duration
		has      bool
	)
	closeAt := func(at time.Duration) {
		if has && at > curStart {
			segments = append(segments, causeSegment{cur, curStart, at})
		}
	}
	for _, ev := range t.Events {
		if ev.Kind != SchedEventGoroutine || ev.Goroutine != g {
			continue
		}
		closeAt(ev.At)
		cur, has = causeOfPlace(ev.Place)
		curStart = ev.At
	}
	closeAt(t.Window)

	var gc []causeSegment
	for _, r := range t.GC {
		if r.Kind == GCStopTheWorld || r.Goroutine == g {
			gc = append(gc, causeSegment{CauseGC, r.Start, r.End})
		}
	}
	gc = mergeSegments(gc)

	var recorded time.Duration
	for _, s := range segments {
		start, end := max(s.start, from), min(s.end, to)
		if end <= start {
			continue
		}
		recorded += end - start
		inGC := overlap(gc, start, end)
		out.Durations[s.cause] += end - start - inGC
		if inGC > 0 {
			out.Durations[CauseGC] += inGC
		}
	}
	for c, d := range out.Durations {
		if d <= 0 {
			delete(out.Durations, c)
		}
	}
	out.Unrecorded = (to - from) - recorded
	return out
}

func mergeSegments(ss []causeSegment) []causeSegment {
	slices.SortFunc(ss, func(a, b causeSegment) int { return int(a.start - b.start) })
	var out []causeSegment
	for _, s := range ss {
		if n := len(out); n > 0 && s.start <= out[n-1].end {
			out[n-1].end = max(out[n-1].end, s.end)
			continue
		}
		out = append(out, s)
	}
	return out
}

func overlap(merged []causeSegment, start, end time.Duration) time.Duration {
	var total time.Duration
	for _, s := range merged {
		if o := min(end, s.end) - max(start, s.start); o > 0 {
			total += o
		}
	}
	return total
}

// TotalByCause は窓の中の全 G の時間を主因ごとに合計する (GC の区間は分けない)。CPU 待ちの量を見るのに使う
func (t SchedulerTimeline) TotalByCause() map[PrimaryCause]time.Duration {
	type cursor struct {
		cause PrimaryCause
		start time.Duration
		ok    bool
	}
	totals := map[PrimaryCause]time.Duration{}
	gs := map[GoroutineID]cursor{}
	for _, ev := range t.Events {
		if ev.Kind != SchedEventGoroutine {
			continue
		}
		if c := gs[ev.Goroutine]; c.ok {
			totals[c.cause] += ev.At - c.start
		}
		cause, ok := causeOfPlace(ev.Place)
		gs[ev.Goroutine] = cursor{cause, ev.At, ok}
	}
	for _, c := range gs {
		if c.ok {
			totals[c.cause] += t.Window - c.start
		}
	}
	return totals
}

// LongestMarkAssist は一番長く GC を手伝わされた G と、その区間の始まり
func (t SchedulerTimeline) LongestMarkAssist() (GoroutineID, time.Duration, bool) {
	per := map[GoroutineID]time.Duration{}
	first := map[GoroutineID]time.Duration{}
	for _, r := range t.GC {
		if r.Kind != GCMarkAssist {
			continue
		}
		if _, ok := first[r.Goroutine]; !ok {
			first[r.Goroutine] = r.Start
		}
		per[r.Goroutine] += r.End - r.Start
	}
	var (
		best    GoroutineID
		longest time.Duration
	)
	for g, d := range per {
		if d > longest || (d == longest && g < best) {
			best, longest = g, d
		}
	}
	return best, first[best], longest > 0
}
