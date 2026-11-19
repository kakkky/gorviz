package domain

import (
	"github.com/kakkky/gorviz/internal/app/api/model"
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
	// CauseCGO は cgo で C のコードを実行していた時間。trace ではシステムコールとして記録されるので、スタックの runtime.cgocall で見分ける
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

func causeOfPlace(p model.GoroutinePlace) (PrimaryCause, bool) {
	switch p.Kind {
	case model.PlaceRunning:
		return CauseRunning, true
	case model.PlaceLocalQueue, model.PlaceGlobalQueue:
		return CauseCPUWait, true
	case model.PlaceWaiting:
		return CauseOfWaitReason(p.Reason), true
	case model.PlaceSyscall:
		if p.Cgo {
			return CauseCGO, true
		}
		return CauseIOWait, true
	}
	return "", false
}

// CauseOfGoroutine は trace がないときに、dump の 1 時点の状態から主因を推し量る
func CauseOfGoroutine(g model.Goroutine) PrimaryCause {
	switch g.StateCategory() {
	case model.GoroutineRunning:
		return CauseRunning
	case model.GoroutineRunnable:
		return CauseCPUWait
	case model.GoroutineSyscall:
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
func Breakdown(t model.SchedulerTimeline, g model.GoroutineID, from, to time.Duration) StateBreakdown {
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
		if ev.Kind != model.SchedEventGoroutine || ev.Goroutine != g {
			continue
		}
		closeAt(ev.At)
		cur, has = causeOfPlace(ev.Place)
		curStart = ev.At
	}
	closeAt(t.Window)

	var gc []causeSegment
	for _, r := range t.GC {
		if (r.Kind == model.GCStopTheWorld && r.ByGC()) || (r.Kind == model.GCMarkAssist && r.Goroutine == g) {
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

// PlaceOfCause は to までに g が cause の状態に入った最後のイベントのうち、スタック (関数と位置) が載ったものを返す。
// 「主因の状態で、どこで止まっていたか」を出すのに使う。区間より前に入った状態が区間の中まで続いていることもあるので、始まりは区切らない
func PlaceOfCause(t model.SchedulerTimeline, g model.GoroutineID, cause PrimaryCause, to time.Duration) (model.SchedEvent, bool) {
	var (
		found model.SchedEvent
		ok    bool
	)
	for _, ev := range t.Events {
		if ev.At > to {
			break
		}
		if ev.Kind != model.SchedEventGoroutine || ev.Goroutine != g || ev.Function == "" {
			continue
		}
		if c, has := causeOfPlace(ev.Place); has && c == cause {
			found, ok = ev, true
		}
	}
	return found, ok
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
func TotalByCause(t model.SchedulerTimeline) map[PrimaryCause]time.Duration {
	type cursor struct {
		cause PrimaryCause
		start time.Duration
		ok    bool
	}
	totals := map[PrimaryCause]time.Duration{}
	gs := map[model.GoroutineID]cursor{}
	for _, ev := range t.Events {
		if ev.Kind != model.SchedEventGoroutine {
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
func LongestMarkAssist(t model.SchedulerTimeline) (model.GoroutineID, time.Duration, bool) {
	per := map[model.GoroutineID]time.Duration{}
	first := map[model.GoroutineID]time.Duration{}
	for _, r := range t.GC {
		if r.Kind != model.GCMarkAssist {
			continue
		}
		if _, ok := first[r.Goroutine]; !ok {
			first[r.Goroutine] = r.Start
		}
		per[r.Goroutine] += r.End - r.Start
	}
	var (
		best    model.GoroutineID
		longest time.Duration
	)
	for g, d := range per {
		if d > longest || (d == longest && g < best) {
			best, longest = g, d
		}
	}
	return best, first[best], longest > 0
}

// StateSinceScan は at の時点で g がいた状態に入った時刻と、その状態の主因を、録画の区切りを新しいほうから 1 つずつさかのぼって探す。
// 録画をまとめて読むと重いので、区切りを 1 つずつ Add し、答えが決まったらやめる。
// trace は状態が変わらなくても G の状態を書き直すことがある (区切りの先頭に持ち越した状態や、途中の状態の書き出し) ので、
// 同じ状態 (場所の種類と待機理由) が続く間はさかのぼり、違う状態から変わった最初のイベントの時刻を答えにする。
// 録画で最初に現れたのが区切りの途中 (持ち越しでない) なら、そこで状態に入った (作られた) とみなす
type StateSinceScan struct {
	g       model.GoroutineID
	at      time.Time
	current model.GoroutinePlace
	found   bool
	carried bool
	done    bool
	since   time.Time
	cause   PrimaryCause
}

func NewStateSinceScan(g model.GoroutineID, at time.Time) *StateSinceScan {
	return &StateSinceScan{g: g, at: at}
}

// Add は前に渡したものより古い区切りを 1 つ読む。答えが決まれば true を返し、それ以上さかのぼらなくてよい
func (s *StateSinceScan) Add(c model.TraceChunk) bool {
	if s.done || c.StartedAt.After(s.at) {
		return s.done
	}
	off := c.Offset(s.at)
	for _, ev := range slices.Backward(c.Timeline.Events) {
		if ev.At > off || ev.Kind != model.SchedEventGoroutine || ev.Goroutine != s.g {
			continue
		}
		if !s.found {
			s.current, s.found = ev.Place, true
			s.cause, _ = causeOfPlace(ev.Place)
		}
		if !samePlace(ev.Place, s.current) {
			s.done = true
			return true
		}
		s.since, s.carried = c.StartedAt.Add(ev.At), ev.At == 0
	}
	return false
}

// Result は状態に入った時刻と、その状態の主因。録画の始まりから同じ状態なら ok は false で cause だけを返し、
// at までに g が録画に現れていなければ cause も空
func (s *StateSinceScan) Result() (since time.Time, cause PrimaryCause, ok bool) {
	if s.done || (s.found && !s.carried) {
		return s.since, s.cause, true
	}
	return time.Time{}, s.cause, false
}

// StateSince は chunks (古い順) をまとめて StateSinceScan に通す
func StateSince(chunks []model.TraceChunk, g model.GoroutineID, at time.Time) (since time.Time, cause PrimaryCause, ok bool) {
	scan := NewStateSinceScan(g, at)
	for _, c := range slices.Backward(chunks) {
		if scan.Add(c) {
			break
		}
	}
	return scan.Result()
}

// samePlace は 2 つの状態が同じか。書き直しのイベントには待機理由が載らないことがあるので、片方の理由が分からなければ場所の種類だけで比べる
func samePlace(a, b model.GoroutinePlace) bool {
	if a.Kind != b.Kind {
		return false
	}
	known := func(r string) bool { return r != "" && r != model.UnknownWaitReason }
	return !known(a.Reason) || !known(b.Reason) || a.Reason == b.Reason
}
