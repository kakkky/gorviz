package domain

import (
	"cmp"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// StateBreakdown は 1 つの goroutine の、ある期間の主因ごとの時間。Unrecorded は期間のうち trace に記録がなかった時間
type StateBreakdown struct {
	Durations  map[MainCause]time.Duration `json:"durations"`
	Unrecorded time.Duration               `json:"unrecorded"`
}

func (b StateBreakdown) Total() time.Duration {
	var t time.Duration
	for _, d := range b.Durations {
		t += d
	}
	return t
}

// Main は最も長い主因。記録が 1 つもなければ false
func (b StateBreakdown) Main() (MainCause, bool) {
	var (
		best    MainCause
		longest time.Duration
	)
	for _, c := range MainCauses {
		if d := b.Durations[c]; d > longest {
			best, longest = c, d
		}
	}
	return best, longest > 0
}

func (b *StateBreakdown) Add(o StateBreakdown) {
	if b.Durations == nil {
		b.Durations = map[MainCause]time.Duration{}
	}
	for c, d := range o.Durations {
		b.Durations[c] += d
	}
	b.Unrecorded += o.Unrecorded
}

// Stretch は 1 つの goroutine が 1 つの主因の状態を途切れずに続けた区間 (チャンクの始まりからの経過時間)。
// Leave は状態を抜けた変化で、待ちから起こされたなら起こした G が載る
type Stretch struct {
	Cause    MainCause
	Start    time.Duration
	End      time.Duration
	Enter    observation.StateChange
	Leave    observation.StateChange
	HasLeave bool
}

func (s Stretch) Duration() time.Duration { return s.End - s.Start }

// Stretches は chunk の中で g が続けた状態の区間を時刻順に返す。同じ主因の書き直しは 1 つにまとめる
func Stretches(c observation.TraceChunk, g observation.GoroutineID) []Stretch {
	var (
		out []Stretch
		cur Stretch
		in  bool
	)
	for _, ev := range c.Events {
		if ev.Kind != observation.StateChangeGoroutine || ev.Goroutine != g {
			continue
		}
		cause, ok := CauseOfPlace(ev.Place, ev.Function)
		if in && ok && cause == cur.Cause {
			if cur.Enter.Function == "" && ev.Function != "" {
				cur.Enter = ev
			}
			continue
		}
		if in {
			cur.End, cur.Leave, cur.HasLeave = ev.At, ev, true
			if cur.End > cur.Start {
				out = append(out, cur)
			}
			in = false
		}
		if ok {
			cur, in = Stretch{Cause: cause, Start: ev.At, Enter: ev}, true
		}
	}
	if in && c.Duration > cur.Start {
		cur.End = c.Duration
		out = append(out, cur)
	}
	return out
}

// StateSince は、ある時点で g がいる主因の状態に入った時刻。Began が false なら、c の頭からその状態で、もっと前から続いている
type StateSince struct {
	Cause MainCause
	Since time.Time
	Began bool
}

// StateSinceAt は c の終わりの時点で g がいる主因の状態と、それに入った時刻。そのとき主因のない状態 (終わった・まだない) なら false
func StateSinceAt(c observation.TraceChunk, g observation.GoroutineID) (StateSince, bool) {
	ss := Stretches(c, g)
	if len(ss) == 0 || ss[len(ss)-1].End < c.Duration {
		return StateSince{}, false
	}
	last := ss[len(ss)-1]
	return StateSince{Cause: last.Cause, Since: c.StartedAt.Add(last.Start), Began: last.Start > 0}, true
}

type span struct{ start, end time.Duration }

func gcSpans(c observation.TraceChunk, g observation.GoroutineID) []span {
	var out []span
	for _, r := range c.GC {
		if (r.Kind == observation.GCStopTheWorld && r.ByGC()) || (r.Kind == observation.GCMarkAssist && r.Goroutine == g) {
			out = append(out, span{r.Start, r.End})
		}
	}
	slices.SortFunc(out, func(a, b span) int { return cmp.Compare(a.start, b.start) })
	var merged []span
	for _, s := range out {
		if n := len(merged); n > 0 && s.start <= merged[n-1].end {
			merged[n-1].end = max(merged[n-1].end, s.end)
			continue
		}
		merged = append(merged, s)
	}
	return merged
}

func overlapOf(spans []span, start, end time.Duration) time.Duration {
	var total time.Duration
	for _, s := range spans {
		if o := min(end, s.end) - max(start, s.start); o > 0 {
			total += o
		}
	}
	return total
}

// Breakdown は chunk の中の [from, to] での g の主因ごとの時間。GC (GC の STW と、その G の mark assist) に重なる時間は、元の状態によらず GC に数える
func Breakdown(c observation.TraceChunk, g observation.GoroutineID, from, to time.Duration) StateBreakdown {
	out := StateBreakdown{Durations: map[MainCause]time.Duration{}}
	from, to = max(from, 0), min(to, c.Duration)
	if to <= from {
		return out
	}
	gc := gcSpans(c, g)
	var recorded time.Duration
	for _, s := range Stretches(c, g) {
		start, end := max(s.Start, from), min(s.End, to)
		if end <= start {
			continue
		}
		recorded += end - start
		inGC := overlapOf(gc, start, end)
		if s.Cause == CauseRunning || s.Cause == CauseRunnable {
			out.Durations[s.Cause] += end - start - inGC
			out.Durations[CauseGC] += inGC
			continue
		}
		out.Durations[s.Cause] += end - start
	}
	for k, d := range out.Durations {
		if d <= 0 {
			delete(out.Durations, k)
		}
	}
	out.Unrecorded = (to - from) - recorded
	return out
}

// StopPlace は止まっていた場所。主因の状態で一番長く続いた区間に入ったときの関数と場所 (file:line)
type StopPlace struct {
	Cause      MainCause     `json:"cause"`
	Function   string        `json:"function"`
	Location   string        `json:"location"`
	WaitReason string        `json:"waitReason,omitempty"`
	Duration   time.Duration `json:"duration"`
}

// LongestStop は [from, to] で、cause の状態が一番長く続いた区間の場所
func LongestStop(c observation.TraceChunk, g observation.GoroutineID, cause MainCause, from, to time.Duration) (StopPlace, bool) {
	var (
		best StopPlace
		ok   bool
	)
	for _, s := range Stretches(c, g) {
		if s.Cause != cause {
			continue
		}
		d := min(s.End, to) - max(s.Start, from)
		if d <= 0 || (ok && d <= best.Duration) {
			continue
		}
		best = StopPlace{Cause: cause, Function: s.Enter.Function, Location: s.Enter.Location, WaitReason: s.Enter.Place.Reason, Duration: d}
		ok = true
	}
	return best, ok
}

// Goroutines は chunk の [from, to] に状態の変化がある goroutine
func Goroutines(c observation.TraceChunk, from, to time.Duration) []observation.GoroutineID {
	seen := map[observation.GoroutineID]bool{}
	var out []observation.GoroutineID
	for _, ev := range c.Events {
		if ev.Kind != observation.StateChangeGoroutine || ev.At < from || ev.At > to || seen[ev.Goroutine] {
			continue
		}
		seen[ev.Goroutine] = true
		out = append(out, ev.Goroutine)
	}
	slices.Sort(out)
	return out
}
