package model

import (
	"cmp"
	"slices"
	"time"
)

// GoroutinePlaceKind は G がスケジューラのどこにいるか。GMP の概念図の置き場所に 1:1 で対応する。
type GoroutinePlaceKind string

const (
	PlaceRunning     GoroutinePlaceKind = "running"
	PlaceLocalQueue  GoroutinePlaceKind = "local_run_queue"
	PlaceGlobalQueue GoroutinePlaceKind = "global_run_queue"
	PlaceWaiting     GoroutinePlaceKind = "waiting"
	PlaceSyscall     GoroutinePlaceKind = "syscall"
	PlaceDead        GoroutinePlaceKind = "dead"
)

// GoroutinePlace の Proc / Thread は Kind が running / local_run_queue / syscall のときだけ意味を持つ。
// HasProc が false の syscall は、P を手放した (handoff された) M の上で syscall 中であることを表す。
// Cgo は syscall が cgo の呼び出し (C のコードの実行) であること。trace では syscall として記録されるので、スタックで見分ける
type GoroutinePlace struct {
	Kind    GoroutinePlaceKind
	Proc    ProcID
	HasProc bool
	Thread  ThreadID
	Reason  string
	Cgo     bool
}

const UnknownWaitReason = "unknown"

// ProcStatus は P の状態。runtime の P にはシステムコール中を表す状態はなく (Go 1.27 の _Psyscall_unused)、
// システムコール中かどうかは G の状態で表す
type ProcStatus string

const (
	ProcIdle    ProcStatus = "idle"
	ProcRunning ProcStatus = "running"
)

type SchedEventKind string

const (
	SchedEventGoroutine SchedEventKind = "goroutine"
	SchedEventProc      SchedEventKind = "proc"
)

// SchedEvent は再生の 1 コマ。Kind が goroutine なら Goroutine が Place に移ったこと、
// proc なら Proc が ProcStatus になったこと (running なら Thread に乗ったこと) を表す。
// Function はその時点で G がいた関数 (スケジューラ内部を除く。sync.(*Mutex).Lock など)、Location はそれを呼んだ利用者のコードの位置 (file:line)。
// trace にスタックが載るイベント (ブロック・停止・syscall) でだけ埋まる。
// Created はこのイベントで G が生まれたこと (窓の中で作られた G) を表す。
// WokenBy は待機から runnable にしたのが別の G なら、その G (unlock・channel の送信など) と、そのときの関数・位置。
// runtime (タイマー・ネットワークのポーラー) が起こしたときは 0
type SchedEvent struct {
	At              time.Duration
	Kind            SchedEventKind
	Goroutine       GoroutineID
	Place           GoroutinePlace
	Function        string
	Location        string
	Created         bool
	WokenBy         GoroutineID
	WokenByFunction string
	WokenByLocation string
	Proc            ProcID
	ProcStatus      ProcStatus
	Thread          ThreadID
}

// ProcSnapshot の RunningInSyscall は、P を持ったまま M の上でシステムコール中の G が Running にいること
type ProcSnapshot struct {
	Proc             ProcID
	Status           ProcStatus
	Thread           ThreadID
	HasThread        bool
	Running          GoroutineID
	HasRunning       bool
	RunningInSyscall bool
	LocalQueue       []GoroutineID
}

type SyscallGoroutine struct {
	Goroutine GoroutineID
	Thread    ThreadID
}

// WaitingGroup は待機中の G をまとめる単位。「何の処理をする G が、何を待って止まっているか」
type WaitingGroup struct {
	Entry  string
	Reason string
}

// SchedState はある時刻の GMP の全体像。
type SchedState struct {
	Procs       []ProcSnapshot
	GlobalQueue []GoroutineID
	Waiting     map[WaitingGroup][]GoroutineID
	// HandedOff は P を手放した M の上で syscall を続けている G
	HandedOff []SyscallGoroutine
	Dead      []GoroutineID
}

type placedGoroutine struct {
	place GoroutinePlace
	seq   int
}

type procCursor struct {
	status    ProcStatus
	thread    ThreadID
	hasThread bool
}

// StateAt は At が t 以下のイベントを順に適用した状態を返す。
// キューの並びは入った順。実際の runtime には runnext (local run queue より先に実行される 1 枠) があるが、trace からは区別できないので扱わない。
func (t SchedulerTimeline) StateAt(at time.Duration) SchedState {
	gs := map[GoroutineID]placedGoroutine{}
	procs := map[ProcID]procCursor{}
	for _, l := range t.Procs {
		procs[l.Proc] = procCursor{status: ProcIdle}
	}
	for i, ev := range t.Events {
		if ev.At > at {
			break
		}
		switch ev.Kind {
		case SchedEventGoroutine:
			gs[ev.Goroutine] = placedGoroutine{place: ev.Place, seq: i}
		case SchedEventProc:
			procs[ev.Proc] = procCursor{status: ev.ProcStatus, thread: ev.Thread, hasThread: ev.ProcStatus == ProcRunning}
		}
	}

	type queued struct {
		g   GoroutineID
		seq int
	}
	var (
		state   = SchedState{Waiting: map[WaitingGroup][]GoroutineID{}}
		local   = map[ProcID][]queued{}
		global  []queued
		running = map[ProcID]GoroutineID{}
		inSys   = map[ProcID]SyscallGoroutine{}
	)
	for g, pg := range gs {
		p := pg.place
		switch p.Kind {
		case PlaceRunning:
			running[p.Proc] = g
		case PlaceLocalQueue:
			local[p.Proc] = append(local[p.Proc], queued{g, pg.seq})
		case PlaceGlobalQueue:
			global = append(global, queued{g, pg.seq})
		case PlaceWaiting:
			key := WaitingGroup{Entry: t.Entries[g], Reason: p.Reason}
			state.Waiting[key] = append(state.Waiting[key], g)
		case PlaceSyscall:
			if p.HasProc {
				inSys[p.Proc] = SyscallGoroutine{Goroutine: g, Thread: p.Thread}
			} else {
				state.HandedOff = append(state.HandedOff, SyscallGoroutine{Goroutine: g, Thread: p.Thread})
			}
		case PlaceDead:
			state.Dead = append(state.Dead, g)
		}
	}

	bySeq := func(a, b queued) int { return cmp.Compare(a.seq, b.seq) }
	ids := func(qs []queued) []GoroutineID {
		slices.SortFunc(qs, bySeq)
		out := make([]GoroutineID, len(qs))
		for i, q := range qs {
			out[i] = q.g
		}
		return out
	}

	for p, c := range procs {
		snap := ProcSnapshot{Proc: p, Status: c.status, Thread: c.thread, HasThread: c.hasThread, LocalQueue: ids(local[p])}
		if g, ok := running[p]; ok {
			snap.Running, snap.HasRunning = g, true
		}
		// P に乗った G が syscall 中なら、P は running のまま、その G をシステムコール中として Running に置く。
		// P が別の M に奪われた (idle になった / 別の G を走らせている) なら、その G は P を手放した M の上にいる (handoff)
		if sg, ok := inSys[p]; ok {
			if c.status == ProcRunning && !snap.HasRunning {
				snap.Running, snap.HasRunning, snap.RunningInSyscall = sg.Goroutine, true, true
			} else {
				state.HandedOff = append(state.HandedOff, sg)
			}
		}
		state.Procs = append(state.Procs, snap)
	}
	slices.SortFunc(state.Procs, func(a, b ProcSnapshot) int { return cmp.Compare(a.Proc, b.Proc) })
	state.GlobalQueue = ids(global)
	for key := range state.Waiting {
		slices.Sort(state.Waiting[key])
	}
	slices.Sort(state.Dead)
	slices.SortFunc(state.HandedOff, func(a, b SyscallGoroutine) int { return cmp.Compare(a.Goroutine, b.Goroutine) })
	return state
}

// Slice は [from, to] の区間を切り出し、時刻 0 を from に合わせた Timeline を返す。
// from 時点の状態は先頭に t=0 のイベントとして積むので、切り出した Timeline だけで StateAt や再生ができる。
// キューの並びを保つため、from より前のイベントは最後に適用された順に並べる。
func (t SchedulerTimeline) Slice(from, to time.Duration) SchedulerTimeline {
	from, to = max(from, 0), min(to, t.Window)
	if to < from {
		to = from
	}
	out := SchedulerTimeline{
		Window:  to - from,
		Threads: t.Threads,
		Entries: t.Entries,
	}
	for _, r := range t.GC {
		if r.End < from || r.Start > to {
			continue
		}
		r.Start, r.End = max(r.Start, from)-from, min(r.End, to)-from
		out.GC = append(out.GC, r)
	}
	for _, h := range t.Heap {
		if h.At < from || h.At > to {
			continue
		}
		h.At -= from
		out.Heap = append(out.Heap, h)
	}

	lastG := map[GoroutineID]int{}
	lastP := map[ProcID]int{}
	rest := len(t.Events)
	for i, ev := range t.Events {
		if ev.At > from {
			rest = i
			break
		}
		switch ev.Kind {
		case SchedEventGoroutine:
			lastG[ev.Goroutine] = i
		case SchedEventProc:
			lastP[ev.Proc] = i
		}
	}
	initial := make([]int, 0, len(lastG)+len(lastP))
	for _, i := range lastP {
		initial = append(initial, i)
	}
	for _, i := range lastG {
		initial = append(initial, i)
	}
	slices.Sort(initial)
	for _, i := range initial {
		ev := t.Events[i]
		ev.At = 0
		out.Events = append(out.Events, ev)
	}
	for _, ev := range t.Events[rest:] {
		if ev.At > to {
			break
		}
		ev.At -= from
		out.Events = append(out.Events, ev)
		if ev.Kind == SchedEventGoroutine && ev.Place.Kind == PlaceDead {
			out.GoroutinesDestroyed++
		}
		if ev.Created {
			out.GoroutinesCreated++
		}
	}

	for _, l := range t.Procs {
		lane := ProcLane{Proc: l.Proc}
		for _, s := range l.Spans {
			if s.End < from || s.Start > to {
				continue
			}
			lane.Spans = append(lane.Spans, SchedSpan{
				Goroutine: s.Goroutine,
				Thread:    s.Thread,
				Start:     max(s.Start, from) - from,
				End:       min(s.End, to) - from,
			})
		}
		out.Procs = append(out.Procs, lane)
	}
	return out
}
