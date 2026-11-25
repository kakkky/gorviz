package domain

import (
	"cmp"
	"slices"
	"time"
)

const impactTop = 5

// ImpactGoroutine は発生の中で影響の大きかった goroutine。Duration は種類ごとの量 (RUNNABLE の時間・mark assist の時間など)
type ImpactGoroutine struct {
	Detail   GoroutineDetail
	Duration time.Duration
	Note     string
}

// LockHolder は、ロックの待ちを終わらせた (ロックを持って待たせていた) 側
type LockHolder struct {
	G        GoroutineID
	Function string
	Location string
	WaitFrom time.Duration
	WaitTo   time.Duration
}

// LockSite は Lock を呼んだ関数ごとの待ち
type LockSite struct {
	Function string
	Location string
	Wait     time.Duration
	Share    float64
	Count    int
	Mean     time.Duration
	Max      time.Duration
	Holders  []LockHolder
}

// Impact は 1 つの発生で、影響の大きかった goroutine と、その内訳
type Impact struct {
	From       time.Duration
	To         time.Duration
	Goroutines []ImpactGoroutine
	LockSites  []LockSite
	Leaks      []LeakStack
}

// ImpactRange は発生の詳細を出す期間。発生の前後を含める
func ImpactRange(ix *TraceIndex, kind IssueKind, o Occurrence) (time.Duration, time.Duration) {
	if kind == IssueGoroutineLeak {
		return max(0, o.To-time.Minute), o.To
	}
	return max(0, o.From-BucketSize/2), min(ix.Last(), o.To+BucketSize/2)
}

func ImpactOf(ix *TraceIndex, is *Issue, o Occurrence) Impact {
	from, to := ImpactRange(ix, is.Kind, o)
	imp := Impact{From: from, To: to}
	var scores map[GoroutineID]time.Duration
	notes := map[GoroutineID]string{}
	switch is.Kind {
	case IssueGoroutineLeak:
		imp.Leaks = o.Leaks
		for _, l := range o.Leaks {
			if len(l.Goroutines) == 0 || len(imp.Goroutines) >= impactTop {
				continue
			}
			g := l.Goroutines[0]
			gfrom, gto := from, o.To
			if rec, ok := ix.Goroutine(g); ok && rec.Created {
				gfrom = max(rec.CreatedAt, from)
			}
			d, err := DetailOf(ix, g, gfrom, gto, o.Leaks)
			if err == nil {
				imp.Goroutines = append(imp.Goroutines, ImpactGoroutine{Detail: d, Note: "one of the leaked goroutines at " + l.Blocked.Location()})
			}
		}
		return imp
	case IssueCPUContention:
		scores = causeTimes(ix, from, to, CauseRunnable)
	case IssueLockContention:
		scores = causeTimes(ix, from, to, CauseLockWait)
		imp.LockSites = lockSites(ix, from, to)
	case IssueGCPressure:
		scores = map[GoroutineID]time.Duration{}
		for _, a := range ix.Assists(from, to) {
			if g, ok := ix.Goroutine(a.G); ok && g.App() {
				for _, iv := range ix.Intervals(g, max(a.Start, from), min(a.End, to)) {
					if iv.Change.Place == PlaceRunning {
						scores[a.G] += iv.End - iv.Start
					}
				}
			}
		}
	case IssueLongSTW:
		scores = map[GoroutineID]time.Duration{}
		for _, s := range ix.STW(from, to) {
			if s.Reason != is.Target || s.End-s.Start < longSTW || s.G == 0 {
				continue
			}
			if s.End-s.Start > scores[s.G] {
				scores[s.G] = s.End - s.Start
				notes[s.G] = "stopped the world (" + s.Reason + ")"
			}
		}
	case IssueThreadExplosion:
		scores = placeTimes(ix, from, to, PlaceSyscall)
	}
	for _, s := range top(scores) {
		d, err := DetailOf(ix, s.g, from, to, nil)
		if err != nil {
			continue
		}
		imp.Goroutines = append(imp.Goroutines, ImpactGoroutine{Detail: d, Duration: s.d, Note: notes[s.g]})
	}
	return imp
}

type scored struct {
	g GoroutineID
	d time.Duration
}

func top(scores map[GoroutineID]time.Duration) []scored {
	out := make([]scored, 0, len(scores))
	for g, d := range scores {
		if d > 0 {
			out = append(out, scored{g: g, d: d})
		}
	}
	slices.SortFunc(out, func(a, b scored) int { return cmp.Or(cmp.Compare(b.d, a.d), cmp.Compare(a.g, b.g)) })
	if len(out) > impactTop {
		out = out[:impactTop]
	}
	return out
}

func eachAppInterval(ix *TraceIndex, from, to time.Duration, f func(g *GoroutineRecord, iv Interval)) {
	for _, id := range ix.GoroutineIDs() {
		g, _ := ix.Goroutine(id)
		if !g.App() || (g.Ended && g.EndedAt < from) || (g.Created && g.CreatedAt > to) {
			continue
		}
		for _, iv := range ix.Intervals(g, from, to) {
			f(g, iv)
		}
	}
}

func causeTimes(ix *TraceIndex, from, to time.Duration, cause MainCause) map[GoroutineID]time.Duration {
	out := map[GoroutineID]time.Duration{}
	eachAppInterval(ix, from, to, func(g *GoroutineRecord, iv Interval) {
		if c, ok := iv.Change.MainCause(); ok && c == cause {
			out[g.ID] += iv.End - iv.Start
		}
	})
	return out
}

func placeTimes(ix *TraceIndex, from, to time.Duration, place PlaceKind) map[GoroutineID]time.Duration {
	out := map[GoroutineID]time.Duration{}
	eachAppInterval(ix, from, to, func(g *GoroutineRecord, iv Interval) {
		if iv.Change.Place == place {
			out[g.ID] += iv.End - iv.Start
		}
	})
	return out
}

// lockSites は Lock の待ちを、Lock を呼んだ関数 (待ちに入ったときの利用者のコードの一番上) ごとにまとめる
func lockSites(ix *TraceIndex, from, to time.Duration) []LockSite {
	type holderKey struct{ fn, loc string }
	sites := map[string]*LockSite{}
	holders := map[string]map[holderKey]*LockHolder{}
	var total time.Duration
	eachAppInterval(ix, from, to, func(g *GoroutineRecord, iv Interval) {
		if c, ok := iv.Change.MainCause(); !ok || c != CauseLockWait {
			return
		}
		f := UserFrame(ix.Stack(iv.Change.Stack))
		key := placeKey(f)
		s, ok := sites[key]
		if !ok {
			s = &LockSite{Function: f.Function, Location: f.Location()}
			sites[key] = s
			holders[key] = map[holderKey]*LockHolder{}
		}
		d := iv.End - iv.Start
		s.Wait += d
		s.Count++
		s.Max = max(s.Max, d)
		total += d
		if iv.HasNext && iv.Next.WokenBy != 0 {
			hf := holderFrame(ix.Stack(iv.Next.Waker), f.Function)
			hk := holderKey{fn: hf.Function, loc: hf.Location()}
			if _, ok := holders[key][hk]; !ok {
				holders[key][hk] = &LockHolder{G: iv.Next.WokenBy, Function: hf.Function, Location: hf.Location(), WaitFrom: iv.Start, WaitTo: iv.End}
			}
		}
	})
	out := make([]LockSite, 0, len(sites))
	for key, s := range sites {
		s.Mean = s.Wait / time.Duration(s.Count)
		if total > 0 {
			s.Share = float64(s.Wait) / float64(total)
		}
		for _, h := range holders[key] {
			s.Holders = append(s.Holders, *h)
		}
		slices.SortFunc(s.Holders, func(a, b LockHolder) int { return cmp.Compare(a.Function, b.Function) })
		if len(s.Holders) > 3 {
			s.Holders = s.Holders[:3]
		}
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b LockSite) int {
		return cmp.Or(cmp.Compare(b.Wait, a.Wait), cmp.Compare(a.Function, b.Function))
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// holderFrame はロックを手放して起こした側のスタックから、待った側と同じ関数 (ロックを包む関数) を飛ばした、利用者のコードの一番上のフレーム。
// 待たせていたのは、ロックを包む関数を呼んでロックを持ったまま仕事をしていた側なので
func holderFrame(stack []Frame, lockFunction string) Frame {
	for _, f := range stack {
		if !IsStdlibFunction(f.Function) && !IsWrapperFunction(f.Function) && f.Function != lockFunction {
			return f
		}
	}
	return UserFrame(stack)
}
