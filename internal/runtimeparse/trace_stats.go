package runtimeparse

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"golang.org/x/exp/trace"
)

// gcWorkerEntry は GC のワーカーの G の入口。この G が P を使っている時間は、アプリではなく GC の時間
const gcWorkerEntry = "runtime.gcBgMarkWorker"

// mmuBucket は GC が P を取った割合を測るときの時間の刻み
const mmuBucket = 100 * time.Microsecond

// waitKey は待ち場所をまとめる単位。待機理由は窓より前から待っている G では分からない (unknown) ので、キーに入れず分かったものを載せる
type waitKey struct {
	kind     model.GoroutinePlaceKind
	cgo      bool
	entry    string
	function string
	location string
}

// openWait の known は待ちに入った時刻が数える範囲の中か。窓より前から続いている待ちは回数に数えず、時間だけを足す
type openWait struct {
	place    model.GoroutinePlace
	function string
	location string
	start    trace.Time
	known    bool
}

type openRunnable struct {
	start trace.Time
	// known は runnable になった時刻が分かっているか。窓より前から runnable だった G は分からないので、待ち時間の分布に入れない
	known bool
}

// statsCounter は状態遷移を全部見て TraceStats を数える。from より前は状態だけを追い、時間は from から数える
type statsCounter struct {
	from     trace.Time
	waiting  map[trace.GoID]openWait
	runnable map[trace.GoID]openRunnable
	waits    map[waitKey]*model.WaitSite
	stats    model.TraceStats
}

func newStatsCounter() *statsCounter {
	return &statsCounter{waiting: map[trace.GoID]openWait{}, runnable: map[trace.GoID]openRunnable{}, waits: map[waitKey]*model.WaitSite{}}
}

func (s *statsCounter) since(t trace.Time) trace.Time {
	return max(t, s.from)
}

// transition は G の状態遷移を 1 つ数える。start はその状態に入った時刻 (窓より前からの状態なら窓の先頭)
func (s *statsCounter) transition(g trace.GoID, from, to trace.GoState, at, start trace.Time, place model.GoroutinePlace, function, location string, entries map[model.GoroutineID]string) {
	switch from {
	case trace.GoWaiting, trace.GoSyscall:
		if w, ok := s.waiting[g]; ok {
			delete(s.waiting, g)
			s.closeWait(g, w, at, entries)
		}
	case trace.GoRunnable:
		if r, ok := s.runnable[g]; ok {
			delete(s.runnable, g)
			if at > s.from {
				s.stats.Runnable += at.Sub(s.since(r.start))
				if to == trace.GoRunning && r.known {
					s.stats.SchedLatency.Observe(at.Sub(r.start))
				}
			}
		}
	}
	switch to {
	case trace.GoWaiting, trace.GoSyscall:
		s.waiting[g] = openWait{place: place, function: function, location: location, start: start, known: from != trace.GoUndetermined && start >= s.from}
	case trace.GoRunnable:
		s.runnable[g] = openRunnable{start: start, known: from != trace.GoUndetermined}
	}
}

func (s *statsCounter) closeWait(g trace.GoID, w openWait, at trace.Time, entries map[model.GoroutineID]string) {
	entry := entries[model.GoroutineID(g)]
	// 場所の分からない待ちと、runtime が自分で持っている G (GC のワーカー・scavenger・trace の書き出しなど) の待ちは数えない
	if at <= s.from || w.location == "" || isRuntimeOwned(entry) || isRuntimeOwned(w.function) {
		return
	}
	d := at.Sub(s.since(w.start))
	key := waitKey{kind: w.place.Kind, cgo: w.place.Cgo, entry: entry, function: w.function, location: w.location}
	site, ok := s.waits[key]
	if !ok {
		site = &model.WaitSite{Kind: key.kind, Cgo: key.cgo, Entry: key.entry, Function: key.function, Location: key.location}
		s.waits[key] = site
	}
	if site.Reason == "" || site.Reason == model.UnknownWaitReason {
		site.Reason = w.place.Reason
	}
	if w.known {
		site.Count++
	}
	site.Total += d
	site.Max = max(site.Max, d)
}

// stw は GC の stop-the-world が 1 回終わったことを数える
func (s *statsCounter) stw(start, end trace.Time) {
	if end > s.from {
		s.stats.STW.Observe(end.Sub(start))
	}
}

// finish は窓の終わりで続いている状態を閉じ、P の使い方と GC が P を取った割合を数える
func (s *statsCounter) finish(begin, last trace.Time, tl model.SchedulerTimeline) model.TraceStats {
	for g, w := range s.waiting {
		s.closeWait(g, w, last, tl.Entries)
	}
	for _, r := range s.runnable {
		if last > s.from {
			s.stats.Runnable += last.Sub(s.since(r.start))
		}
	}
	for _, w := range s.waits {
		s.stats.Waits = append(s.stats.Waits, *w)
	}
	slices.SortFunc(s.stats.Waits, func(a, b model.WaitSite) int {
		return cmp.Or(cmp.Compare(b.Total, a.Total), cmp.Compare(a.Location, b.Location), cmp.Compare(a.Entry, b.Entry))
	})

	from := max(s.from.Sub(begin), 0)
	s.stats.Window = max(tl.Window-from, 0)
	s.stats.Procs = len(tl.Procs)
	if s.stats.Window <= 0 || s.stats.Procs == 0 {
		return s.stats
	}
	gc := newProcShare(from, tl.Window)
	for _, l := range tl.Procs {
		for _, sp := range l.Spans {
			start, end := max(sp.Start, from), sp.End
			if end <= start {
				continue
			}
			if tl.Entries[sp.Goroutine] == gcWorkerEntry {
				s.stats.ProcGC += end - start
				gc.add(start, end, 1)
				continue
			}
			s.stats.ProcRunning += end - start
		}
	}
	for _, r := range tl.GC {
		start, end := max(r.Start, from), r.End
		if end <= start || !r.ByGC() {
			continue
		}
		switch r.Kind {
		case model.GCStopTheWorld:
			s.stats.ProcGC += (end - start) * time.Duration(s.stats.Procs)
			gc.add(start, end, float64(s.stats.Procs))
		case model.GCMarkAssist:
			gc.add(start, end, 1)
		}
	}
	s.stats.GCMaxShare = gc.maxShare(model.MMUWindow, s.stats.Procs)
	return s.stats
}

// procShare は mmuBucket ごとに、GC が使った P の時間を足し込む
type procShare struct {
	from    time.Duration
	buckets []float64
}

func newProcShare(from, to time.Duration) *procShare {
	return &procShare{from: from, buckets: make([]float64, int((to-from)/mmuBucket)+1)}
}

// add は [start, end] に P を procs 個ぶん GC が使ったことを足す
func (p *procShare) add(start, end time.Duration, procs float64) {
	for i := int((start - p.from) / mmuBucket); i < len(p.buckets); i++ {
		bs := p.from + time.Duration(i)*mmuBucket
		if bs >= end {
			break
		}
		if o := min(end, bs+mmuBucket) - max(start, bs); o > 0 {
			p.buckets[i] += float64(o) * procs
		}
	}
}

// maxShare は window の長さの窓をずらしながら、GC が全 P の時間を取った割合の最大を返す
func (p *procShare) maxShare(window time.Duration, procs int) float64 {
	n := min(int(window/mmuBucket), len(p.buckets))
	if n == 0 {
		return 0
	}
	var sum, best float64
	for i, v := range p.buckets {
		sum += v
		if i >= n {
			sum -= p.buckets[i-n]
		}
		if i >= n-1 {
			best = max(best, sum)
		}
	}
	return min(best/(float64(time.Duration(n)*mmuBucket)*float64(procs)), 1)
}

func isRuntimeOwned(fn string) bool {
	return model.IsRuntimeFunction(fn) || strings.HasPrefix(fn, "runtime/")
}

// userFrame は利用者のコード (標準ライブラリと runtime を除く) の一番上のフレーム。なければ currentFrame
func userFrame(frames []trace.StackFrame) trace.StackFrame {
	for _, f := range frames {
		if !model.IsStdlibFunction(f.Func) {
			return f
		}
	}
	return currentFrame(frames)
}
