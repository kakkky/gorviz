package client

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/trace"

	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/slogtrace"
)

// 再生用イベントの上限。ブラウザに JSON で送って毎フレーム適用するので、忙しいプロセスで窓が長いと重くなりすぎる
const maxReplayEvents = 50_000

type gcRangeKey struct {
	kind domain.GCRangeKind
	g    trace.GoID
}

type runningSpan struct {
	proc   domain.ProcID
	thread domain.ThreadID
	start  trace.Time
}

// buildTraceChunk は execution trace の状態遷移から「どの G がどの M 上でどの P を使って走ったか」の区間と再生用のイベントを組み立てる。
// G が Running に入ったときに区間を開き、Running から出たときに閉じる。
// receivedAt は trace に壁時計の記録がない (Go 1.25 未満) ときに、取り終えた時刻から逆算するのに使う。
func buildTraceChunk(r io.Reader, receivedAt time.Time) (domain.TraceChunk, error) {
	tr, err := trace.NewReader(r)
	if err != nil {
		return domain.TraceChunk{}, domain.ErrInternal.Wrap(err, "open execution trace")
	}
	var (
		chunk domain.TraceChunk
		clock *trace.ClockSnapshot
	)

	var (
		tl      = domain.SchedulerTimeline{Entries: map[domain.GoroutineID]string{}}
		begin   trace.Time
		last    trace.Time
		started bool
		running = map[trace.GoID]runningSpan{}
		initial []domain.SchedEvent
		lanes   = map[domain.ProcID]*domain.ProcLane{}
		// trace の ThreadID は OS のスレッド ID で桁が大きく読みにくいので、観測窓の中で現れた順に 0 から振り直す
		threads  = map[trace.ThreadID]domain.ThreadID{}
		threadOf = func(th trace.ThreadID) domain.ThreadID {
			id, ok := threads[th]
			if !ok {
				id = domain.ThreadID(len(threads))
				threads[th] = id
			}
			return id
		}
		openGC = map[gcRangeKey]trace.Time{}
		heap   domain.HeapSample
		laneOf = func(p domain.ProcID) *domain.ProcLane {
			l, ok := lanes[p]
			if !ok {
				l = &domain.ProcLane{Proc: p}
				lanes[p] = l
			}
			return l
		}
	)
	closeSpan := func(g trace.GoID, at trace.Time) {
		s, ok := running[g]
		if !ok {
			return
		}
		delete(running, g)
		laneOf(s.proc).Spans = append(laneOf(s.proc).Spans, domain.SchedSpan{
			Goroutine: domain.GoroutineID(g),
			Thread:    s.thread,
			Start:     s.start.Sub(begin),
			End:       at.Sub(begin),
		})
	}

	for {
		ev, err := tr.ReadEvent()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return domain.TraceChunk{}, domain.ErrInternal.Wrap(err, "read execution trace")
		}
		if !started {
			begin, started = ev.Time(), true
		}
		last = ev.Time()
		if ev.Thread() != trace.NoThread {
			threadOf(ev.Thread())
		}
		switch ev.Kind() {
		case trace.EventSync:
			if clock == nil {
				clock = ev.Sync().ClockSnapshot
			}
			continue
		case trace.EventLog:
			if l := ev.Log(); l.Category == slogtrace.TraceCategory {
				if n, err := strconv.ParseUint(l.Message, 10, 64); err == nil {
					chunk.LogMarks = append(chunk.LogMarks, domain.TraceLogMark{Seq: n, Goroutine: domain.GoroutineID(ev.Goroutine()), At: ev.Time().Sub(begin)})
				}
			}
			continue
		case trace.EventRangeBegin, trace.EventRangeActive, trace.EventRangeEnd:
			r := ev.Range()
			key, ok := gcRangeKeyOf(r)
			if r.Name == "GC concurrent mark phase" && ev.Kind() == trace.EventRangeBegin {
				tl.GCCycles++
			}
			if !ok {
				continue
			}
			switch ev.Kind() {
			case trace.EventRangeBegin:
				openGC[key] = ev.Time()
			case trace.EventRangeActive:
				// 窓より前から続いている区間は、窓の先頭から始まったとみなす
				openGC[key] = begin
			case trace.EventRangeEnd:
				if start, ok := openGC[key]; ok {
					delete(openGC, key)
					tl.GC = append(tl.GC, domain.GCRange{Kind: key.kind, Goroutine: domain.GoroutineID(key.g), Start: start.Sub(begin), End: ev.Time().Sub(begin)})
				}
			}
			continue
		case trace.EventMetric:
			m := ev.Metric()
			switch m.Name {
			case "/memory/classes/heap/objects:bytes":
				heap.LiveBytes = m.Value.Uint64()
			case "/gc/heap/goal:bytes":
				heap.GoalBytes = m.Value.Uint64()
			default:
				continue
			}
			heap.At = ev.Time().Sub(begin)
			tl.Heap = append(tl.Heap, heap)
			continue
		case trace.EventStateTransition:
		default:
			continue
		}

		st := ev.StateTransition()
		at := ev.Time().Sub(begin)
		// 窓より前から続いている状態は、その G / P に初めて触れたときや窓の終わりにまとめて記録される。
		// これらは窓の先頭からその状態だったとみなし、t=0 のイベントとして先頭に寄せる
		fromUndetermined := false
		switch st.Resource.Kind {
		case trace.ResourceProc:
			from, _ := st.Proc()
			fromUndetermined = from == trace.ProcUndetermined
		case trace.ResourceGoroutine:
			from, _ := st.Goroutine()
			fromUndetermined = from == trace.GoUndetermined
		}
		emit := func(e domain.SchedEvent) {
			if len(initial)+len(tl.Events) >= maxReplayEvents {
				tl.EventsTruncated = true
				return
			}
			if fromUndetermined {
				initial = append(initial, e)
				return
			}
			e.At = at
			tl.Events = append(tl.Events, e)
		}
		switch st.Resource.Kind {
		case trace.ResourceProc:
			p := domain.ProcID(st.Resource.Proc())
			laneOf(p)
			_, to := st.Proc()
			switch to {
			case trace.ProcRunning:
				emit(domain.SchedEvent{Kind: domain.SchedEventProc, Proc: p, ProcStatus: domain.ProcRunning, Thread: threadOf(ev.Thread())})
			case trace.ProcIdle:
				emit(domain.SchedEvent{Kind: domain.SchedEventProc, Proc: p, ProcStatus: domain.ProcIdle})
			}
		case trace.ResourceGoroutine:
			g := st.Resource.Goroutine()
			from, to := st.Goroutine()
			frames := stackFrames(st.Stack)
			// 生成時のスタックは新しい G の入口から始まる。窓の前からいた G の状態記録のスタックは、その G 自身の今のスタック
			if len(frames) > 0 && (from == trace.GoNotExist || from == trace.GoUndetermined) {
				tl.Entries[domain.GoroutineID(g)] = frames[len(frames)-1].Func
			}
			var function, location string
			if from != trace.GoNotExist && len(frames) > 0 {
				f := currentFrame(frames)
				function, location = f.Func, fmt.Sprintf("%s:%d", f.File, f.Line)
			}
			if place, ok := placeOf(ev, st, from, to, threadOf); ok {
				place.Cgo = place.Kind == domain.PlaceSyscall && isCgoCall(frames)
				emit(domain.SchedEvent{Kind: domain.SchedEventGoroutine, Goroutine: domain.GoroutineID(g), Place: place, Function: function, Location: location, Created: from == trace.GoNotExist})
			}
			if from == trace.GoNotExist && to != trace.GoNotExist {
				tl.GoroutinesCreated++
			}
			if to == trace.GoNotExist && from != trace.GoUndetermined {
				tl.GoroutinesDestroyed++
			}
			switch {
			case to == trace.GoRunning && from != trace.GoRunning:
				if ev.Proc() == trace.NoProc {
					continue
				}
				running[g] = runningSpan{
					proc:   domain.ProcID(ev.Proc()),
					thread: threadOf(ev.Thread()),
					start:  ev.Time(),
				}
			case from == trace.GoRunning && to != trace.GoRunning:
				closeSpan(g, ev.Time())
			}
		}
	}

	for g := range running {
		closeSpan(g, last)
	}
	for key, start := range openGC {
		tl.GC = append(tl.GC, domain.GCRange{Kind: key.kind, Goroutine: domain.GoroutineID(key.g), Start: start.Sub(begin), End: last.Sub(begin)})
	}
	slices.SortFunc(tl.GC, func(a, b domain.GCRange) int { return cmp.Compare(a.Start, b.Start) })
	tl.Events = append(initial, tl.Events...)
	tl.Window = last.Sub(begin)
	for _, l := range lanes {
		tl.Procs = append(tl.Procs, *l)
	}
	for _, th := range threads {
		tl.Threads = append(tl.Threads, th)
	}
	chunk.Timeline = tl
	if clock != nil {
		chunk.StartedAt = clock.Wall.Add(begin.Sub(clock.Trace))
	} else {
		chunk.StartedAt = receivedAt.Add(-tl.Window)
	}
	return chunk, nil
}

// isCgoCall は syscall に入ったスタックが cgo の呼び出し (runtime.cgocall) か
func isCgoCall(frames []trace.StackFrame) bool {
	return slices.ContainsFunc(frames, func(f trace.StackFrame) bool { return f.Func == "runtime.cgocall" })
}

func stackFrames(s trace.Stack) []trace.StackFrame {
	var frames []trace.StackFrame
	for f := range s.Frames() {
		frames = append(frames, f)
	}
	return frames
}

// currentFrame は domain.CurrentFunction と同じ規則で、スケジューラ内部を除いた一番上のフレームを返す
func currentFrame(frames []trace.StackFrame) trace.StackFrame {
	for _, f := range frames {
		if !domain.IsRuntimeFunction(f.Func) {
			return f
		}
	}
	return frames[0]
}

// gcRangeKeyOf は trace の区間のうち、G の時間を GC に取られたものを見分ける。
// STW は止めた G に紐づいて記録されるが、全 G が止まるので G を問わない区間として扱う
func gcRangeKeyOf(r trace.Range) (gcRangeKey, bool) {
	switch {
	case strings.HasPrefix(r.Name, "stop-the-world"):
		return gcRangeKey{kind: domain.GCStopTheWorld}, true
	case r.Name == "GC mark assist" && r.Scope.Kind == trace.ResourceGoroutine:
		return gcRangeKey{kind: domain.GCMarkAssist, g: r.Scope.Goroutine()}, true
	}
	return gcRangeKey{}, false
}

// placeOf は G の状態遷移を、概念図上の置き場所に読み替える。runtime の run queue の中身は trace に出ないので、
// runnable になった G の行き先は runtime の振る舞いから推測する:
//   - 生成・unblock された G は、それを行った P の local run queue に積まれる (P を持たない文脈なら global run queue)
//   - 横取り (preempt) や Gosched で Running から降ろされた G は global run queue に積まれる
//   - syscall から戻ったときに P を取り戻せなかった G は global run queue に積まれる
func placeOf(ev trace.Event, st trace.StateTransition, from, to trace.GoState, threadOf func(trace.ThreadID) domain.ThreadID) (domain.GoroutinePlace, bool) {
	proc, hasProc := domain.ProcID(ev.Proc()), ev.Proc() != trace.NoProc
	switch to {
	case trace.GoRunning:
		if !hasProc {
			return domain.GoroutinePlace{}, false
		}
		return domain.GoroutinePlace{Kind: domain.PlaceRunning, Proc: proc, HasProc: true, Thread: threadOf(ev.Thread())}, true
	case trace.GoRunnable:
		if from == trace.GoRunning || from == trace.GoSyscall || !hasProc {
			return domain.GoroutinePlace{Kind: domain.PlaceGlobalQueue, Reason: st.Reason}, true
		}
		return domain.GoroutinePlace{Kind: domain.PlaceLocalQueue, Proc: proc, HasProc: true}, true
	case trace.GoWaiting:
		reason := st.Reason
		if reason == "" {
			reason = domain.UnknownWaitReason
		}
		return domain.GoroutinePlace{Kind: domain.PlaceWaiting, Reason: reason}, true
	case trace.GoSyscall:
		return domain.GoroutinePlace{Kind: domain.PlaceSyscall, Proc: proc, HasProc: hasProc, Thread: threadOf(ev.Thread())}, true
	case trace.GoNotExist:
		if from == trace.GoUndetermined {
			return domain.GoroutinePlace{}, false
		}
		return domain.GoroutinePlace{Kind: domain.PlaceDead}, true
	}
	return domain.GoroutinePlace{}, false
}
