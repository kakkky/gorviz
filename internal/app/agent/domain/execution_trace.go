package domain

import (
	"bytes"
	"cmp"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/trace"

	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
)

// ParsedTrace は生の execution trace を読んだもの。Goroutines はチャンクの終わりの時点の G ごとの状態で、スナップショットの材料になる
type ParsedTrace struct {
	Chunk          observation.TraceChunk
	Goroutines     map[observation.GoroutineID]GoroutineAtEnd
	MaxGoroutineID observation.GoroutineID
}

// GoroutineAtEnd の Determined は、チャンクの中で今の状態に移ったのを見たか。見ていなければ (チャンクの終わりの状態の記録だけなら)、
// Since と Reason は分からない。Stack は G 自身のスタックで、走り出した後は分からないので空にする
type GoroutineAtEnd struct {
	Alive        bool
	State        observation.GoroutineState
	Determined   bool
	Since        time.Time
	Reason       string
	Stack        []observation.Frame
	CreatedBy    observation.Frame
	HasCreatedBy bool
	WokenBy      observation.GoroutineID
	WokenByAt    observation.Frame
}

type gcRangeKey struct {
	kind   observation.GCRangeKind
	g      trace.GoID
	reason string
}

type runningSpan struct {
	proc   observation.ProcID
	thread observation.ThreadID
	start  trace.Time
}

// ParseExecutionTrace は生の trace をチャンクに整形する。receivedAt は trace に壁時計の記録がないときに、取り終えた時刻から逆算するのに使う。
// since より前は落とす。FlightRecorder から切り出した trace は前のチャンクと重なるため
func ParseExecutionTrace(raw []byte, receivedAt, since time.Time) (ParsedTrace, error) {
	b := newTraceBuilder()
	if err := b.read(bytes.NewReader(raw), since); err != nil {
		return ParsedTrace{}, err
	}
	return b.finish(receivedAt, since), nil
}

type traceBuilder struct {
	chunk    observation.TraceChunk
	clock    *trace.ClockSnapshot
	begin    trace.Time
	last     trace.Time
	started  bool
	stats    *statsCounter
	running  map[trace.GoID]runningSpan
	initial  []observation.StateChange
	lanes    map[observation.ProcID]*observation.ProcLane
	threads  map[trace.ThreadID]observation.ThreadID
	openGC   map[gcRangeKey]trace.Time
	heap     observation.HeapSample
	gcBegins []trace.Time
	// awaitingGoal は GC が終わって、次の heap goal をまだ読んでいない間 true
	awaitingGoal bool
	goroutines   map[trace.GoID]*goroutineTrack
	maxG         trace.GoID
}

// goroutineTrack の at は今の状態に移った trace の時刻 (determined のときだけ)
type goroutineTrack struct {
	end GoroutineAtEnd
	at  trace.Time
	// lockWait は今の待ちが sync のロック・WaitGroup・Cond か。起こした G を残すのはこの待ちだけ
	lockWait bool
}

func newTraceBuilder() *traceBuilder {
	return &traceBuilder{
		chunk:      observation.TraceChunk{Entries: map[observation.GoroutineID]string{}},
		stats:      newStatsCounter(),
		running:    map[trace.GoID]runningSpan{},
		lanes:      map[observation.ProcID]*observation.ProcLane{},
		threads:    map[trace.ThreadID]observation.ThreadID{},
		openGC:     map[gcRangeKey]trace.Time{},
		goroutines: map[trace.GoID]*goroutineTrack{},
	}
}

// threadOf は trace の ThreadID が OS のスレッド ID で桁が大きく読みにくいので、チャンクの中で現れた順に 0 から振り直す
func (b *traceBuilder) threadOf(th trace.ThreadID) observation.ThreadID {
	id, ok := b.threads[th]
	if !ok {
		id = observation.ThreadID(len(b.threads))
		b.threads[th] = id
	}
	return id
}

func (b *traceBuilder) laneOf(p observation.ProcID) *observation.ProcLane {
	l, ok := b.lanes[p]
	if !ok {
		l = &observation.ProcLane{Proc: p}
		b.lanes[p] = l
	}
	return l
}

func (b *traceBuilder) closeSpan(g trace.GoID, at trace.Time) {
	s, ok := b.running[g]
	if !ok {
		return
	}
	delete(b.running, g)
	l := b.laneOf(s.proc)
	l.Spans = append(l.Spans, observation.ProcSpan{Goroutine: observation.GoroutineID(g), Thread: s.thread, Start: s.start.Sub(b.begin), End: at.Sub(b.begin)})
}

func (b *traceBuilder) read(r io.Reader, since time.Time) error {
	tr, err := trace.NewReader(r)
	if err != nil {
		return ErrInvalidArgument.Wrap(err, "open execution trace")
	}
	for {
		ev, err := tr.ReadEvent()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return ErrInvalidArgument.Wrap(err, "read execution trace")
		}
		if !b.started {
			b.begin, b.started = ev.Time(), true
			b.stats.from = b.begin
		}
		b.last = ev.Time()
		if ev.Thread() != trace.NoThread {
			b.threadOf(ev.Thread())
		}
		switch ev.Kind() {
		case trace.EventSync:
			if b.clock == nil {
				b.clock = ev.Sync().ClockSnapshot
				if b.clock != nil && !since.IsZero() {
					b.stats.from = max(b.stats.from, b.clock.Trace+trace.Time(since.Sub(b.clock.Wall)))
				}
			}
		case trace.EventLog:
			if l := ev.Log(); l.Category == loggerhook.Category {
				if n, err := strconv.ParseUint(l.Message, 10, 64); err == nil {
					b.chunk.LogMarks = append(b.chunk.LogMarks, observation.LogMark{Seq: n, Goroutine: observation.GoroutineID(ev.Goroutine()), At: ev.Time().Sub(b.begin)})
				}
			}
		case trace.EventRangeBegin, trace.EventRangeActive, trace.EventRangeEnd:
			b.rangeEvent(ev)
		case trace.EventMetric:
			b.metric(ev)
		case trace.EventStateTransition:
			b.transition(ev)
		}
	}
}

func (b *traceBuilder) rangeEvent(ev trace.Event) {
	r := ev.Range()
	if r.Name == "GC concurrent mark phase" {
		switch ev.Kind() {
		case trace.EventRangeBegin:
			b.gcBegins = append(b.gcBegins, ev.Time())
		case trace.EventRangeEnd:
			// runtime は mark の終わりに heap objects を生き残った量で置き直してから区間を閉じ、そのあと次の goal を出す。
			// なので直前の heap objects が live heap になる。チャンクの始まりでまだ読んでいなければ分からない
			if b.heap.ObjectsBytes > 0 {
				b.chunk.GCEnds = append(b.chunk.GCEnds, observation.GCEnd{At: ev.Time().Sub(b.begin), LiveHeapBytes: b.heap.ObjectsBytes})
				b.awaitingGoal = true
			}
		}
	}
	key, ok := gcRangeKeyOf(r)
	if !ok {
		return
	}
	switch ev.Kind() {
	case trace.EventRangeBegin:
		b.openGC[key] = ev.Time()
	case trace.EventRangeActive:
		// チャンクより前から続いている区間は、チャンクの先頭から始まったとみなす
		b.openGC[key] = b.begin
	case trace.EventRangeEnd:
		start, ok := b.openGC[key]
		if !ok {
			return
		}
		delete(b.openGC, key)
		if key.kind == observation.GCStopTheWorld && strings.HasPrefix(key.reason, "GC ") {
			b.stats.stw(start, ev.Time())
		}
		b.chunk.GC = append(b.chunk.GC, observation.GCRange{Kind: key.kind, Goroutine: observation.GoroutineID(key.g), Reason: key.reason, Start: start.Sub(b.begin), End: ev.Time().Sub(b.begin)})
	}
}

func (b *traceBuilder) metric(ev trace.Event) {
	m := ev.Metric()
	switch m.Name {
	case "/memory/classes/heap/objects:bytes":
		b.heap.ObjectsBytes = m.Value.Uint64()
	case "/gc/heap/goal:bytes":
		b.heap.GoalBytes = m.Value.Uint64()
		if b.awaitingGoal {
			b.chunk.GCEnds[len(b.chunk.GCEnds)-1].HeapGoalBytes = b.heap.GoalBytes
			b.awaitingGoal = false
		}
	default:
		return
	}
	b.heap.At = ev.Time().Sub(b.begin)
	b.chunk.Heap = append(b.chunk.Heap, b.heap)
}

func (b *traceBuilder) transition(ev trace.Event) {
	st := ev.StateTransition()
	at := ev.Time().Sub(b.begin)
	// チャンクより前から続いている状態は、その G / P に初めて触れたときやチャンクの終わりにまとめて記録される。
	// これらはチャンクの先頭からその状態だったとみなし、時刻 0 のイベントとして先頭に寄せる
	fromUndetermined := false
	switch st.Resource.Kind {
	case trace.ResourceProc:
		from, _ := st.Proc()
		fromUndetermined = from == trace.ProcUndetermined
	case trace.ResourceGoroutine:
		from, _ := st.Goroutine()
		fromUndetermined = from == trace.GoUndetermined
	}
	emit := func(e observation.StateChange) {
		if fromUndetermined {
			b.initial = append(b.initial, e)
			return
		}
		e.At = at
		b.chunk.Events = append(b.chunk.Events, e)
	}
	switch st.Resource.Kind {
	case trace.ResourceProc:
		p := observation.ProcID(st.Resource.Proc())
		b.laneOf(p)
		_, to := st.Proc()
		switch to {
		case trace.ProcRunning:
			emit(observation.StateChange{Kind: observation.StateChangeProc, Proc: p, ProcStatus: observation.ProcRunning, Thread: b.threadOf(ev.Thread())})
		case trace.ProcIdle:
			emit(observation.StateChange{Kind: observation.StateChangeProc, Proc: p, ProcStatus: observation.ProcIdle})
		}
	case trace.ResourceGoroutine:
		b.goroutineTransition(ev, st, emit, fromUndetermined)
	}
}

func (b *traceBuilder) goroutineTransition(ev trace.Event, st trace.StateTransition, emit func(observation.StateChange), fromUndetermined bool) {
	g := st.Resource.Goroutine()
	b.maxG = max(b.maxG, g)
	from, to := st.Goroutine()
	frames := stackFrames(st.Stack)
	gid := observation.GoroutineID(g)
	// 世代の切り替わりごとに、runtime は状態の変わっていない G の状態も書き直す。変化ではないので、スタックだけを受け取る
	if from == to {
		if t, ok := b.goroutines[g]; ok && len(frames) > 0 {
			t.end.Stack = frames
		}
		return
	}
	// 生成時のスタックは新しい G の入口から始まる。チャンクの前からいた G の状態の記録のスタックは、その G 自身の今のスタック。
	// go 文の関数がラッパー (pprof.Do など) なら、生成時のスタックにはラッパーしか載らないので、後でスタックが載ったときに入口を決め直す
	if entry, ok := b.chunk.Entries[gid]; len(frames) > 0 && (from == trace.GoNotExist || from == trace.GoUndetermined || (ok && observation.IsWrapperFunction(entry))) {
		b.chunk.Entries[gid] = observation.EntryFunction(frames)
	}
	var function, location string
	if from != trace.GoNotExist && len(frames) > 0 {
		function, location = observation.CurrentFrame(frames).Function, observation.UserFrame(frames).Location()
	}
	place, placed := b.placeOf(ev, st, from, to)
	place.Cgo = place.Kind == observation.PlaceSyscall && isCgoCall(frames)
	start := ev.Time()
	if fromUndetermined {
		start = b.begin
	}
	b.stats.transition(g, from, to, ev.Time(), start, place, function, location, b.chunk.Entries)
	var woken *observation.StateChange
	if placed {
		e := observation.StateChange{Kind: observation.StateChangeGoroutine, Goroutine: gid, Place: place, Function: function, Location: location, Created: from == trace.GoNotExist}
		// 待機から起こしたのが別の G なら、イベントはその G の上で起き、スタックもその G のもの
		if from == trace.GoWaiting && to == trace.GoRunnable && ev.Goroutine() != trace.NoGoroutine && ev.Goroutine() != g {
			e.WokenBy = observation.GoroutineID(ev.Goroutine())
			if wf := stackFrames(ev.Stack()); len(wf) > 0 {
				e.WokenByFunction, e.WokenByLocation = observation.CurrentFrame(wf).Function, observation.UserFrame(wf).Location()
			}
			woken = &e
		}
		emit(e)
	}
	b.track(ev, gid, from, to, place, frames, woken, fromUndetermined)
	switch {
	case to == trace.GoRunning && from != trace.GoRunning:
		if ev.Proc() == trace.NoProc {
			return
		}
		b.running[g] = runningSpan{proc: observation.ProcID(ev.Proc()), thread: b.threadOf(ev.Thread()), start: ev.Time()}
	case from == trace.GoRunning && to != trace.GoRunning:
		b.closeSpan(g, ev.Time())
	}
}

// track は G ごとに、チャンクの終わりの状態と、そこに至った事情 (いつ・なぜ・誰が作った・誰が起こした) を覚える
func (b *traceBuilder) track(ev trace.Event, g observation.GoroutineID, from, to trace.GoState, place observation.Place, frames []observation.Frame, woken *observation.StateChange, fromUndetermined bool) {
	t, ok := b.goroutines[trace.GoID(g)]
	if !ok {
		t = &goroutineTrack{}
		b.goroutines[trace.GoID(g)] = t
	}
	if from == trace.GoNotExist && to != trace.GoNotExist {
		if cf := stackFrames(ev.Stack()); len(cf) > 0 {
			if creator := observation.CurrentFrame(cf); !observation.IsWrapperFunction(creator.Function) {
				t.end.CreatedBy, t.end.HasCreatedBy = creator, true
			}
		}
	}
	state, alive := goroutineState(to)
	t.end.Alive = alive
	if !alive {
		return
	}
	// 終わりの状態の記録は、チャンクの中で見た変化と同じ状態なら、見た変化の時刻と理由を残す
	if fromUndetermined && t.end.Determined && t.end.State == state {
		if len(frames) > 0 {
			t.end.Stack = frames
		}
		return
	}
	if t.end.State != state || !fromUndetermined {
		t.end.Stack = nil
	}
	t.end.State = state
	t.end.Determined = !fromUndetermined
	t.at = ev.Time()
	t.end.Reason = ""
	if state == observation.GoroutineWaiting && !fromUndetermined {
		t.end.Reason = place.Reason
	}
	if len(frames) > 0 {
		t.end.Stack = frames
	}
	switch {
	case to == trace.GoWaiting:
		t.lockWait = isLockWait(place.Reason)
		t.end.WokenBy, t.end.WokenByAt = 0, observation.Frame{}
	case woken != nil && t.lockWait:
		t.end.WokenBy = woken.WokenBy
		t.end.WokenByAt = observation.Frame{Function: woken.WokenByFunction}
		if wf := stackFrames(ev.Stack()); len(wf) > 0 {
			t.end.WokenByAt = observation.UserFrame(wf)
		}
		t.lockWait = false
	}
}

func goroutineState(s trace.GoState) (observation.GoroutineState, bool) {
	switch s {
	case trace.GoRunning:
		return observation.GoroutineRunning, true
	case trace.GoRunnable:
		return observation.GoroutineRunnable, true
	case trace.GoWaiting:
		return observation.GoroutineWaiting, true
	case trace.GoSyscall:
		return observation.GoroutineSyscall, true
	}
	return "", false
}

// isLockWait は trace の待機理由のうち、sync のロック・WaitGroup・セマフォ・Cond の待ち
func isLockWait(reason string) bool {
	return reason == "sync" || reason == "sync.(*Cond).Wait"
}

func (b *traceBuilder) finish(receivedAt, since time.Time) ParsedTrace {
	for g := range b.running {
		b.closeSpan(g, b.last)
	}
	for key, start := range b.openGC {
		b.chunk.GC = append(b.chunk.GC, observation.GCRange{Kind: key.kind, Goroutine: observation.GoroutineID(key.g), Reason: key.reason, Start: start.Sub(b.begin), End: b.last.Sub(b.begin)})
	}
	slices.SortFunc(b.chunk.GC, func(a, c observation.GCRange) int { return cmp.Compare(a.Start, c.Start) })
	for i := range b.initial {
		b.initial[i].At = 0
	}
	b.chunk.Events = append(b.initial, b.chunk.Events...)
	b.chunk.Duration = b.last.Sub(b.begin)
	for _, l := range b.lanes {
		b.chunk.Procs = append(b.chunk.Procs, *l)
	}
	slices.SortFunc(b.chunk.Procs, func(a, c observation.ProcLane) int { return cmp.Compare(a.Proc, c.Proc) })
	for _, e := range b.chunk.Events {
		if e.Kind != observation.StateChangeGoroutine {
			continue
		}
		if e.Created {
			b.chunk.GoroutinesCreated++
		}
		if e.Place.Kind == observation.PlaceDead {
			b.chunk.GoroutinesDestroyed++
		}
	}
	for _, t := range b.gcBegins {
		if t >= b.stats.from {
			b.chunk.GCCycles++
		}
	}
	b.chunk.Stats = b.stats.finish(b.begin, b.last, b.chunk)
	if b.clock != nil {
		b.chunk.StartedAt = b.clock.Wall.Add(b.begin.Sub(b.clock.Trace))
	} else {
		b.chunk.StartedAt = receivedAt.Add(-b.chunk.Duration)
	}
	out := ParsedTrace{Chunk: b.chunk, Goroutines: make(map[observation.GoroutineID]GoroutineAtEnd, len(b.goroutines)), MaxGoroutineID: observation.GoroutineID(b.maxG)}
	for g, t := range b.goroutines {
		end := t.end
		if end.Determined {
			end.Since = b.chunk.StartedAt.Add(t.at.Sub(b.begin))
		}
		out.Goroutines[observation.GoroutineID(g)] = end
	}
	if !since.IsZero() && since.After(b.chunk.StartedAt) {
		out.Chunk = trimBefore(b.chunk, since)
	}
	return out
}

// trimBefore は since より前を落とす。集計と GC の回数は since から数えたものを、そのまま残す
func trimBefore(c observation.TraceChunk, since time.Time) observation.TraceChunk {
	out := c.Slice(c.Offset(since), c.Duration)
	out.Stats, out.GCCycles = c.Stats, c.GCCycles
	return out
}

func isCgoCall(frames []observation.Frame) bool {
	return slices.ContainsFunc(frames, func(f observation.Frame) bool { return f.Function == "runtime.cgocall" })
}

func stackFrames(s trace.Stack) []observation.Frame {
	var frames []observation.Frame
	for f := range s.Frames() {
		frames = append(frames, observation.Frame{Function: f.Func, File: f.File, Line: int(f.Line)})
	}
	return frames
}

// gcRangeKeyOf は trace の区間のうち、GC の区間を見分ける (STW・mark assist・concurrent mark)。
// STW は止めた G に紐づいて記録されるが、全 G が止まるので G を問わない区間として扱う
func gcRangeKeyOf(r trace.Range) (gcRangeKey, bool) {
	switch {
	case r.Name == "GC concurrent mark phase":
		return gcRangeKey{kind: observation.GCMark}, true
	case strings.HasPrefix(r.Name, "stop-the-world"):
		reason := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(r.Name, "stop-the-world"), " ("), ")")
		return gcRangeKey{kind: observation.GCStopTheWorld, reason: reason}, true
	case r.Name == "GC mark assist" && r.Scope.Kind == trace.ResourceGoroutine:
		return gcRangeKey{kind: observation.GCMarkAssist, g: r.Scope.Goroutine()}, true
	}
	return gcRangeKey{}, false
}

// placeOf は G の状態の変化を、概念図上の置き場所に読み替える。runtime の run queue の中身は trace に出ないので、
// runnable になった G の行き先は runtime の振る舞いから推測する:
//   - 生成・unblock された G は、それを行った P の local run queue に積まれる (P を持たない文脈なら global run queue)
//   - 横取り (preempt) や Gosched で Running から降ろされた G は global run queue に積まれる
//   - syscall から戻ったときに P を取り戻せなかった G は global run queue に積まれる
func (b *traceBuilder) placeOf(ev trace.Event, st trace.StateTransition, from, to trace.GoState) (observation.Place, bool) {
	proc, hasProc := observation.ProcID(ev.Proc()), ev.Proc() != trace.NoProc
	switch to {
	case trace.GoRunning:
		if !hasProc {
			return observation.Place{}, false
		}
		return observation.Place{Kind: observation.PlaceRunning, Proc: proc, HasProc: true, Thread: b.threadOf(ev.Thread())}, true
	case trace.GoRunnable:
		if from == trace.GoRunning || from == trace.GoSyscall || !hasProc {
			return observation.Place{Kind: observation.PlaceGlobalQueue, Reason: st.Reason}, true
		}
		return observation.Place{Kind: observation.PlaceLocalQueue, Proc: proc, HasProc: true}, true
	case trace.GoWaiting:
		reason := st.Reason
		if reason == "" {
			reason = observation.UnknownWaitReason
		}
		return observation.Place{Kind: observation.PlaceWaiting, Reason: reason}, true
	case trace.GoSyscall:
		return observation.Place{Kind: observation.PlaceSyscall, Proc: proc, HasProc: hasProc, Thread: b.threadOf(ev.Thread())}, true
	case trace.GoNotExist:
		if from == trace.GoUndetermined {
			return observation.Place{}, false
		}
		return observation.Place{Kind: observation.PlaceDead}, true
	}
	return observation.Place{}, false
}
