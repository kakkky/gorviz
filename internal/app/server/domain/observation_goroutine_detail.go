package domain

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

type CauseTime struct {
	Cause    MainCause
	Duration time.Duration
}

// Waker は待ちを終わらせて起こした goroutine と、起こした場所。WaitFrom・WaitTo は起こされた側が待っていた区間
type Waker struct {
	G        GoroutineID
	Function string
	Location string
	Cause    MainCause
	WaitFrom time.Duration
	WaitTo   time.Duration
	Count    int
	Total    time.Duration
}

// GoroutineDetail は、ある期間に 1 つの goroutine が何に止まっていたか
type GoroutineDetail struct {
	ID              GoroutineID
	Entry           string
	Creator         Frame
	CreatedAt       time.Duration
	Created         bool
	Gorviz          bool
	Ended           bool
	EndedAt         time.Duration
	From            time.Duration
	To              time.Duration
	Breakdown       []CauseTime
	MainCause       MainCause
	Certainty       Certainty
	StoppedAt       Frame
	StoppedIn       string
	StoppedFor      time.Duration
	StoppedReason   string
	Stack           []Frame
	Wakers          []Waker
	Leak            *LeakStack
	TraceTruncated  bool
	HasObservations bool
}

// GoroutineNotFound は見つからない goroutine の ID に、近い ID を添えたエラー
func GoroutineNotFound(ix *ObservationSet, id GoroutineID) error {
	var near []GoroutineID
	for _, g := range ix.GoroutineIDs() {
		if d := g - id; d >= -5 && d <= 5 {
			near = append(near, g)
		}
	}
	slices.Sort(near)
	if len(near) == 0 {
		return ErrNotFound.With(fmt.Sprintf("goroutine %d was not seen in this session", id))
	}
	return ErrNotFound.With(fmt.Sprintf("goroutine %d was not seen in this session; nearby goroutines are %v", id, near))
}

// DetailOf は goroutine の from から to までの詳細を出す
func DetailOf(ix *ObservationSet, id GoroutineID, from, to time.Duration, leaks []LeakStack) (GoroutineDetail, error) {
	g, ok := ix.Goroutine(id)
	if !ok {
		return GoroutineDetail{}, GoroutineNotFound(ix, id)
	}
	_, broken := ix.Ended()
	d := GoroutineDetail{ID: g.ID, Entry: g.Entry, Creator: g.Creator, CreatedAt: g.CreatedAt, Created: g.Created, Gorviz: g.Gorviz, Ended: g.Ended, EndedAt: g.EndedAt,
		From: from, To: min(to, ix.Last()), TraceTruncated: broken, Certainty: CertaintyFact}
	byCause := map[MainCause]time.Duration{}
	inferred := map[MainCause]bool{}
	type stop struct {
		frame  Frame
		in     string
		reason string
		total  time.Duration
		stack  StackID
	}
	stops := map[string]*stop{}
	wakers := map[GoroutineID]*Waker{}
	for _, iv := range ix.Intervals(g, from, to) {
		cause, ok := iv.Change.MainCause()
		if !ok {
			continue
		}
		dur := iv.End - iv.Start
		byCause[cause] += dur
		if iv.Change.Inferred {
			inferred[cause] = true
		}
		if cause != CauseRunning {
			frames := ix.Stack(iv.Change.Stack)
			f := UserFrame(frames)
			key := placeKey(f) + "|" + string(cause)
			s, ok := stops[key]
			if !ok {
				s = &stop{frame: f, in: CurrentFrame(frames).Function, reason: ix.Reason(iv.Change.Reason), stack: iv.Change.Stack}
				stops[key] = s
			}
			s.total += dur
		}
		if (cause == CauseLockWait || cause == CauseSyncWait) && iv.HasNext && iv.Next.WokenBy != 0 {
			w, ok := wakers[iv.Next.WokenBy]
			if !ok {
				f := UserFrame(ix.Stack(iv.Next.Waker))
				w = &Waker{G: iv.Next.WokenBy, Function: f.Function, Location: f.Location(), Cause: cause, WaitFrom: iv.Start, WaitTo: iv.End}
				wakers[iv.Next.WokenBy] = w
			}
			w.Count++
			w.Total += dur
			if dur > w.WaitTo-w.WaitFrom {
				w.WaitFrom, w.WaitTo = iv.Start, iv.End
			}
		}
	}
	for _, c := range MainCauses {
		if byCause[c] > 0 {
			d.Breakdown = append(d.Breakdown, CauseTime{Cause: c, Duration: byCause[c]})
			d.HasObservations = true
		}
	}
	if len(d.Breakdown) > 0 {
		main := d.Breakdown[0]
		for _, ct := range d.Breakdown[1:] {
			if ct.Duration > main.Duration {
				main = ct
			}
		}
		d.MainCause = main.Cause
		if inferred[main.Cause] {
			d.Certainty = CertaintyInference
		}
	}
	var longest *stop
	for _, s := range stops {
		if longest == nil || s.total > longest.total || s.total == longest.total && placeKey(s.frame) < placeKey(longest.frame) {
			longest = s
		}
	}
	if longest != nil {
		d.StoppedAt, d.StoppedFor, d.StoppedReason, d.Stack = longest.frame, longest.total, longest.reason, ix.Stack(longest.stack)
		if longest.in != longest.frame.Function {
			d.StoppedIn = longest.in
		}
	}
	for _, w := range wakers {
		d.Wakers = append(d.Wakers, *w)
	}
	slices.SortFunc(d.Wakers, func(a, b Waker) int { return cmp.Or(cmp.Compare(b.Total, a.Total), cmp.Compare(a.G, b.G)) })
	if len(d.Wakers) > 5 {
		d.Wakers = d.Wakers[:5]
	}
	if l, ok := LeakOf(leaks, id); ok {
		d.Leak = &l
	}
	return d, nil
}

// DefaultDetailRange は期間を指定されずに goroutine を開いたときの期間。goroutine がいた期間全体を、直近の 1 分までに絞る
func DefaultDetailRange(ix *ObservationSet, id GoroutineID) (time.Duration, time.Duration) {
	to := ix.Last()
	g, ok := ix.Goroutine(id)
	if ok && g.Ended {
		to = g.EndedAt
	}
	from := max(0, to-time.Minute)
	if ok && g.Created && g.CreatedAt > from {
		from = g.CreatedAt
	}
	return from, to
}
