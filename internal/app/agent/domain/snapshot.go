package domain

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// GoroutineMemory はインスタンスごとに、trace のチャンクの終わりの記録には入らないので goroutine ごとに覚えておく情報
type GoroutineMemory map[observation.GoroutineID]RememberedGoroutine

// RememberedGoroutine の SinceLowerBound は、今の状態に入ったのを見ておらず、Since が agent が見始めた時刻であること
type RememberedGoroutine struct {
	State              observation.GoroutineState
	Since              time.Time
	SinceLowerBound    bool
	WaitReason         string
	WaitReasonInferred bool
	CreatedBy          observation.Frame
	HasCreatedBy       bool
}

// BuildSnapshot はチャンクの終わりの時点で生きている全 goroutine を組み立て、次のチャンクに持ち越す情報を返す。終わった goroutine は忘れる
func BuildSnapshot(parsed ParsedTrace, memory GoroutineMemory) (observation.GoroutineSnapshot, GoroutineMemory) {
	end := parsed.Chunk.EndedAt()
	snap := observation.GoroutineSnapshot{At: end}
	next := make(GoroutineMemory, len(parsed.Goroutines))
	for id, g := range parsed.Goroutines {
		if !g.Alive {
			continue
		}
		r := remember(memory[id], g, parsed.Chunk.StartedAt)
		next[id] = r
		entry := observation.EntryFunction(g.Stack)
		if entry == "" {
			entry = parsed.Chunk.Entries[id]
		}
		out := observation.Goroutine{
			ID:                id,
			State:             r.State,
			Waiting:           max(end.Sub(r.Since), 0),
			WaitingLowerBound: r.SinceLowerBound,
			Entry:             entry,
			Current:           observation.CurrentFrame(g.Stack),
			CreatedBy:         r.CreatedBy,
			Stack:             g.Stack,
			WokenBy:           g.WokenBy,
			WokenByAt:         g.WokenByAt,
		}
		if r.State == observation.GoroutineWaiting {
			out.WaitReason, out.WaitReasonInferred = r.WaitReason, r.WaitReasonInferred
		}
		if !r.HasCreatedBy {
			out.CreatedBy, out.CreatedByInferred = observation.Frame{Function: entry}, true
		}
		snap.Goroutines = append(snap.Goroutines, out)
	}
	slices.SortFunc(snap.Goroutines, func(a, b observation.Goroutine) int { return cmp.Compare(a.ID, b.ID) })
	return snap, next
}

func remember(r RememberedGoroutine, g GoroutineAtEnd, chunkStart time.Time) RememberedGoroutine {
	if g.HasCreatedBy {
		r.CreatedBy, r.HasCreatedBy = g.CreatedBy, true
	}
	switch {
	case g.Determined:
		r.State, r.Since, r.SinceLowerBound = g.State, g.Since, false
		r.WaitReason, r.WaitReasonInferred = g.Reason, false
	case r.State != g.State || r.Since.IsZero():
		r.State, r.Since, r.SinceLowerBound = g.State, chunkStart, true
		r.WaitReason, r.WaitReasonInferred = "", false
	}
	if r.State != observation.GoroutineWaiting {
		r.WaitReason, r.WaitReasonInferred = "", false
		return r
	}
	if !knownWaitReason(r.WaitReason) || r.WaitReasonInferred {
		r.WaitReason, r.WaitReasonInferred = InferWaitReason(g.Stack), true
	}
	return r
}

func knownWaitReason(r string) bool {
	return r != "" && r != observation.UnknownWaitReason
}

// inferRules は待機に入った runtime の関数から、trace の待機理由の言い方で理由を推定する。上のフレームから順に当てる
var inferRules = []struct {
	prefix string
	reason string
}{
	{"runtime.chanrecv", "chan receive"},
	{"runtime.chansend", "chan send"},
	{"runtime.selectgo", "select"},
	{"runtime.block", "forever"},
	{"sync.(*Cond).Wait", "sync.(*Cond).Wait"},
	{"runtime.notifyListWait", "sync.(*Cond).Wait"},
	{"sync.runtime_Semacquire", "sync"},
	{"internal/sync.runtime_Semacquire", "sync"},
	{"runtime.semacquire", "sync"},
	{"time.Sleep", "sleep"},
	{"runtime.timeSleep", "sleep"},
	{"internal/poll.runtime_pollWait", "network"},
	{"runtime.netpollblock", "network"},
	{"runtime.gcBgMarkWorker", "system goroutine wait"},
	{"runtime.bgsweep", "GC background sweeper wait"},
	{"runtime.bgscavenge", "GC background sweeper wait"},
	{"runtime.forcegchelper", "system goroutine wait"},
	{"runtime.runFinalizers", "system goroutine wait"},
	{"runtime.runCleanups", "system goroutine wait"},
}

// InferWaitReason は agent が見始める前から待っている goroutine の待機理由を、スタックの上の方の関数から推定する。当てはまらなければ unknown
func InferWaitReason(stack []observation.Frame) string {
	for _, f := range stack {
		for _, r := range inferRules {
			if strings.HasPrefix(f.Function, r.prefix) {
				return r.reason
			}
		}
	}
	return observation.UnknownWaitReason
}

// InheritWaitReasons はチャンクの先頭に寄せた状態の記録のうち、理由の分からない待ちを、前のチャンクまでに覚えた理由で補う
func InheritWaitReasons(chunk *observation.TraceChunk, memory GoroutineMemory) {
	for i := range chunk.Events {
		ev := &chunk.Events[i]
		if ev.At > 0 {
			return
		}
		if ev.Kind != observation.StateChangeGoroutine || ev.Place.Kind != observation.PlaceWaiting || knownWaitReason(ev.Place.Reason) {
			continue
		}
		if r, ok := memory[ev.Goroutine]; ok && r.State == observation.GoroutineWaiting && knownWaitReason(r.WaitReason) && !r.WaitReasonInferred {
			ev.Place.Reason = r.WaitReason
		}
	}
}
