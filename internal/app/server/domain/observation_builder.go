package domain

import (
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/protocol/agent"
	"golang.org/x/exp/trace"
)

// goroutineState は、組み立てている間だけ持つ goroutine の今の状態。acct は、その秒の集計にどこまで足したか
type goroutineState struct {
	rec       *GoroutineRecord
	last      StateChange
	place     PlaceKind
	cause     MainCause
	since     time.Duration
	acct      time.Duration
	placed    bool
	assisting bool
}

type rangeKey struct {
	name string
	g    GoroutineID
}

// ObservationBuilder は、流れてくる execution trace を読み進め、1 秒が閉じるたびに Observation を組んで返す。
// 読み進めるのに要る途中の状態 (goroutine の今の状態・開いている区間・スタックの表) は、読んでいる間だけ持つ
type ObservationBuilder struct {
	begin   trace.Time
	started bool
	last    time.Duration
	maxProc int16

	goroutines map[GoroutineID]*goroutineState
	alive      map[GoroutineID]*goroutineState
	procs      map[int16]ProcChange
	openRanges map[rangeKey]time.Duration

	stacks     [][]Frame
	stackKeys  map[string]StackID
	stackCache map[trace.Stack]StackID
	reasons    []string
	reasonIdx  map[string]uint16
	threads    map[trace.ThreadID]int16

	cur       Observation
	touched   map[GoroutineID]bool
	appAlive  int
	runnable  int
	threadNow int
	heapNow   HeapSample

	newMarks      []TraceMark
	newLeakChecks map[uint64]time.Duration
}

func NewObservationBuilder() *ObservationBuilder {
	b := &ObservationBuilder{
		goroutines:    map[GoroutineID]*goroutineState{},
		alive:         map[GoroutineID]*goroutineState{},
		procs:         map[int16]ProcChange{},
		openRanges:    map[rangeKey]time.Duration{},
		stacks:        [][]Frame{nil},
		stackKeys:     map[string]StackID{},
		stackCache:    map[trace.Stack]StackID{},
		reasons:       []string{""},
		reasonIdx:     map[string]uint16{"": 0},
		threads:       map[trace.ThreadID]int16{},
		maxProc:       -1,
		newLeakChecks: map[uint64]time.Duration{},
	}
	b.open(0)
	return b
}

func (b *ObservationBuilder) Started() bool { return b.started }

// TakeNews は、前に呼んでから trace に現れた出来事と、goroutineleak プロファイルを取った時刻を返す。秒が閉じるのを待たずに伝えるため
func (b *ObservationBuilder) TakeNews() ([]TraceMark, map[uint64]time.Duration) {
	marks, checks := b.newMarks, b.newLeakChecks
	if len(marks) == 0 && len(checks) == 0 {
		return nil, nil
	}
	b.newMarks, b.newLeakChecks = nil, map[uint64]time.Duration{}
	return marks, checks
}

// open は second 秒目を始める。秒の頭に生きていた goroutine と P の状態を Start・ProcStart に写す
func (b *ObservationBuilder) open(second int) {
	b.cur = Observation{Second: second, LeakChecks: map[uint64]time.Duration{}, Stacks: map[StackID][]Frame{},
		Stats: Bucket{Goroutines: b.appAlive, Runnable: b.runnable, Threads: b.threadNow, HeapObjects: b.heapNow.Objects, HeapGoal: b.heapNow.Goal}}
	b.touched = map[GoroutineID]bool{}
	for id, g := range b.alive {
		if g.placed {
			b.cur.Start = append(b.cur.Start, g.last)
			b.touch(id)
		}
	}
	for _, p := range b.procs {
		b.cur.ProcStart = append(b.cur.ProcStart, p)
	}
}

func (b *ObservationBuilder) touch(id GoroutineID) {
	b.touched[id] = true
}

// close は今の秒を閉じて返す。続いている状態は、秒の終わりまでをこの秒の集計に足す
func (b *ObservationBuilder) close(end time.Duration) Observation {
	for _, g := range b.alive {
		if g.placed {
			b.account(g, end)
		}
	}
	o := b.cur
	o.Procs = int(b.maxProc) + 1
	o.Stats.Goroutines, o.Stats.Runnable, o.Stats.Threads = b.appAlive, b.runnable, b.threadNow
	o.Stats.HeapObjects, o.Stats.HeapGoal = b.heapNow.Objects, b.heapNow.Goal
	for id := range b.touched {
		g := b.goroutines[id]
		o.Goroutines = append(o.Goroutines, *g.rec)
		b.addStack(&o, g.last.Stack)
	}
	slices.SortFunc(o.Goroutines, func(x, y GoroutineRecord) int { return int(x.ID - y.ID) })
	for _, c := range o.Start {
		b.addStack(&o, c.Stack)
	}
	for _, c := range o.Changes {
		b.addStack(&o, c.Stack)
		b.addStack(&o, c.Waker)
	}
	o.Reasons = slices.Clone(b.reasons)
	return o
}

func (b *ObservationBuilder) addStack(o *Observation, id StackID) {
	if id != 0 {
		o.Stacks[id] = b.stacks[id]
	}
}

// advance は at を含む秒まで進め、その間に閉じた秒を返す
func (b *ObservationBuilder) advance(at time.Duration) []Observation {
	var out []Observation
	for second := int(at / BucketSize); b.cur.Second < second; {
		end := time.Duration(b.cur.Second+1) * BucketSize
		out = append(out, b.close(end))
		b.open(b.cur.Second + 1)
	}
	return out
}

// Apply は 1 つのイベントを足し、それで閉じた秒の Observation を返す
func (b *ObservationBuilder) Apply(ev trace.Event) []Observation {
	if !b.started {
		b.begin, b.started = ev.Time(), true
	}
	at := ev.Time().Sub(b.begin)
	closed := b.advance(at)
	if at > b.last {
		b.last = at
	}
	switch ev.Kind() {
	case trace.EventSync:
		clear(b.stackCache)
	case trace.EventLog:
		b.log(ev, at)
	case trace.EventRangeBegin, trace.EventRangeActive, trace.EventRangeEnd:
		b.rangeEvent(ev, at)
	case trace.EventMetric:
		b.metric(ev, at)
	case trace.EventStateTransition:
		st := ev.StateTransition()
		switch st.Resource.Kind {
		case trace.ResourceProc:
			b.procTransition(ev, st, at)
		case trace.ResourceGoroutine:
			b.goroutineTransition(ev, st, at)
		}
	}
	return closed
}

// End は trace の終わり。開いている区間を閉じ、最後の秒を返す
func (b *ObservationBuilder) End() (Observation, time.Duration) {
	for k, start := range b.openRanges {
		b.closeRange(k, start, b.last)
	}
	b.openRanges = map[rangeKey]time.Duration{}
	return b.close(b.last), b.last
}

func (b *ObservationBuilder) threadOf(th trace.ThreadID) int16 {
	if th == trace.NoThread {
		return -1
	}
	id, ok := b.threads[th]
	if !ok {
		id = int16(len(b.threads))
		b.threads[th] = id
	}
	return id
}

func (b *ObservationBuilder) reasonOf(r string) uint16 {
	if i, ok := b.reasonIdx[r]; ok {
		return i
	}
	i := uint16(len(b.reasons))
	b.reasons = append(b.reasons, r)
	b.reasonIdx[r] = i
	return i
}

func (b *ObservationBuilder) stackOf(s trace.Stack) StackID {
	if s == trace.NoStack {
		return 0
	}
	if id, ok := b.stackCache[s]; ok {
		return id
	}
	var frames []Frame
	var key strings.Builder
	for f := range s.Frames() {
		frames = append(frames, Frame{Function: f.Func, File: f.File, Line: int(f.Line)})
		key.WriteString(f.Func)
		key.WriteByte(':')
		key.WriteString(strconv.FormatUint(f.Line, 10))
		key.WriteByte(';')
	}
	id, ok := b.stackKeys[key.String()]
	if !ok {
		id = StackID(len(b.stacks))
		b.stacks = append(b.stacks, frames)
		b.stackKeys[key.String()] = id
	}
	b.stackCache[s] = id
	return id
}

func (b *ObservationBuilder) log(ev trace.Event, at time.Duration) {
	l := ev.Log()
	if l.Category != agent.TraceCategory {
		return
	}
	switch {
	case strings.HasPrefix(l.Message, agent.EventThreadCountPrefix):
		if n, err := strconv.Atoi(strings.TrimPrefix(l.Message, agent.EventThreadCountPrefix)); err == nil {
			b.threadNow = n
		}
	case strings.HasPrefix(l.Message, agent.EventLeakCheckPrefix):
		if n, err := strconv.ParseUint(strings.TrimPrefix(l.Message, agent.EventLeakCheckPrefix), 10, 64); err == nil {
			b.cur.LeakChecks[n] = at
			b.newLeakChecks[n] = at
		}
	default:
		m := TraceMark{At: at, Message: l.Message}
		b.cur.Marks = append(b.cur.Marks, m)
		b.newMarks = append(b.newMarks, m)
	}
}

func (b *ObservationBuilder) metric(ev trace.Event, at time.Duration) {
	m := ev.Metric()
	switch m.Name {
	case "/memory/classes/heap/objects:bytes":
		b.heapNow.Objects = m.Value.Uint64()
	case "/gc/heap/goal:bytes":
		b.heapNow.Goal = m.Value.Uint64()
	default:
		return
	}
	b.heapNow.At = at
	b.cur.Heap = append(b.cur.Heap, b.heapNow)
}

func rangeKeyOf(ev trace.Event) (rangeKey, bool) {
	r := ev.Range()
	switch {
	case r.Name == "GC concurrent mark phase":
		return rangeKey{name: r.Name}, true
	case strings.HasPrefix(r.Name, "stop-the-world"):
		return rangeKey{name: r.Name}, true
	case r.Name == "GC mark assist" && r.Scope.Kind == trace.ResourceGoroutine:
		return rangeKey{name: r.Name, g: GoroutineID(r.Scope.Goroutine())}, true
	}
	return rangeKey{}, false
}

func (b *ObservationBuilder) rangeEvent(ev trace.Event, at time.Duration) {
	key, ok := rangeKeyOf(ev)
	if !ok {
		return
	}
	switch ev.Kind() {
	case trace.EventRangeBegin, trace.EventRangeActive:
		if _, open := b.openRanges[key]; !open {
			b.openRanges[key] = at
			b.setAssisting(key, at, true)
		}
	case trace.EventRangeEnd:
		start, open := b.openRanges[key]
		if !open {
			return
		}
		delete(b.openRanges, key)
		b.setAssisting(key, at, false)
		if strings.HasPrefix(key.name, "stop-the-world") {
			key.g = GoroutineID(ev.Goroutine())
		}
		b.closeRange(key, start, at)
	}
}

// setAssisting は mark assist の始まりと終わり。mark assist の区間は assist の順番を待って止まっている間も続くので、走っていた分だけを CPU として数えられるよう、印を付け替える前にそこまでを集計する
func (b *ObservationBuilder) setAssisting(key rangeKey, at time.Duration, on bool) {
	if key.name != "GC mark assist" {
		return
	}
	if g, ok := b.goroutines[key.g]; ok {
		if g.placed {
			b.account(g, at)
		}
		g.assisting = on
	}
}

func (b *ObservationBuilder) byAgent(id GoroutineID) bool {
	g, ok := b.goroutines[id]
	return ok && g.rec.Agent
}

func (b *ObservationBuilder) closeRange(key rangeKey, start, end time.Duration) {
	switch {
	case key.name == "GC concurrent mark phase":
		b.cur.GCMark = append(b.cur.GCMark, TimeRange{Start: start, End: end})
	case key.name == "GC mark assist":
		b.cur.Assists = append(b.cur.Assists, TimeRange{Start: start, End: end, G: key.g})
	case strings.HasPrefix(key.name, "stop-the-world"):
		reason := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(key.name, "stop-the-world"), " ("), ")")
		b.cur.STW = append(b.cur.STW, TimeRange{Start: start, End: end, G: key.g, Reason: reason})
		// agent が goroutineleak プロファイルのために走らせた GC などの STW は、アプリの問題ではないので数えない (再生には出す)
		if b.byAgent(key.g) {
			return
		}
		d := end - max(start, b.cur.From())
		b.cur.Stats.STW += max(0, d)
		b.cur.Stats.STWMax = max(b.cur.Stats.STWMax, end-start)
	}
}

func (b *ObservationBuilder) procTransition(ev trace.Event, st trace.StateTransition, at time.Duration) {
	p := int16(st.Resource.Proc())
	b.maxProc = max(b.maxProc, p)
	_, to := st.Proc()
	var c ProcChange
	switch to {
	case trace.ProcRunning:
		c = ProcChange{At: at, Proc: p, Running: true, Thread: b.threadOf(ev.Thread())}
	case trace.ProcIdle:
		c = ProcChange{At: at, Proc: p, Thread: -1}
	default:
		return
	}
	b.procs[p] = c
	b.cur.ProcChanges = append(b.cur.ProcChanges, c)
}

func (b *ObservationBuilder) goroutineTransition(ev trace.Event, st trace.StateTransition, at time.Duration) {
	id := GoroutineID(st.Resource.Goroutine())
	from, to := st.Goroutine()
	if from == to {
		return
	}
	stack := b.stackOf(st.Stack)
	g, known := b.goroutines[id]
	if !known {
		g = &goroutineState{rec: &GoroutineRecord{ID: id}}
		b.goroutines[id] = g
		b.identify(g.rec, ev, from, stack, at)
	} else if IsWrapperFunction(g.rec.Entry) && stack != 0 {
		// go 文の関数がラッパー (scope など) なら、生成時のスタックにはラッパーしか載らないので、後でスタックが載ったときに入口を決め直す
		g.rec.Entry = EntryFunction(b.stacks[stack])
	}
	b.touch(id)
	if (from == trace.GoNotExist || from == trace.GoUndetermined) && !g.placed {
		b.alive[id] = g
		if g.rec.App() {
			b.appAlive++
		}
	}
	change := StateChange{At: at, G: id, Stack: stack, Proc: -1, Thread: b.threadOf(ev.Thread())}
	if ev.Proc() != trace.NoProc {
		change.Proc = int16(ev.Proc())
		b.maxProc = max(b.maxProc, change.Proc)
	}
	var cause MainCause
	switch to {
	case trace.GoRunning:
		change.Place, cause = PlaceRunning, CauseRunning
	case trace.GoRunnable:
		change.Place, cause = PlaceLocalQueue, CauseRunnable
		// runtime の run queue の中身は trace に出ないので、行き先は runtime の振る舞いから推測する。
		// 横取り・Gosched・syscall からの戻りで P を取れなかった G は global run queue に、生成・unblock された G は行った P の local run queue に積まれる
		if from == trace.GoRunning || from == trace.GoSyscall || ev.Proc() == trace.NoProc {
			change.Place = PlaceGlobalQueue
			change.Proc = -1
		}
	case trace.GoWaiting:
		change.Place = PlaceWaiting
		reason := st.Reason
		frames := b.stacks[stack]
		if from == trace.GoUndetermined && reason == "" {
			reason = InferWaitReason(frames)
			change.Inferred = true
		}
		if reason == "" {
			reason = "unknown"
		}
		change.Reason = b.reasonOf(reason)
		cause = CauseOfWait(reason, frames)
	case trace.GoSyscall:
		change.Place, cause = PlaceSyscall, CauseIOWait
		if isCgoStack(b.stacks[stack]) {
			cause = CauseCgo
		}
	case trace.GoNotExist:
		change.Place = PlaceDead
	default:
		return
	}
	if from == trace.GoWaiting && to == trace.GoRunnable && ev.Goroutine() != trace.NoGoroutine && GoroutineID(ev.Goroutine()) != id {
		change.WokenBy = GoroutineID(ev.Goroutine())
		change.Waker = b.stackOf(ev.Stack())
		b.touch(change.WokenBy)
	}
	if g.placed {
		b.account(g, at)
	}
	if g.rec.App() {
		wasRunnable := g.placed && (g.place == PlaceLocalQueue || g.place == PlaceGlobalQueue)
		nowRunnable := change.Place == PlaceLocalQueue || change.Place == PlaceGlobalQueue
		if to == trace.GoRunning && wasRunnable {
			b.cur.Stats.RunnableWait.Add(at - g.since)
		}
		if wasRunnable != nowRunnable {
			if nowRunnable {
				b.runnable++
			} else {
				b.runnable--
			}
		}
	}
	change.Cause = causeIndex(cause)
	b.cur.Changes = append(b.cur.Changes, change)
	g.last = change
	g.place, g.cause, g.since, g.acct, g.placed = change.Place, cause, at, at, true
	if to == trace.GoNotExist {
		g.rec.Ended, g.rec.EndedAt = true, at
		delete(b.alive, id)
		if g.rec.App() {
			b.appAlive--
		}
	}
}

// identify は初めて見た goroutine の素性を決める。作られるのを見た G は生成時のスタックが入口から始まり、イベントのスタックは go 文を書いた側のもの。
// trace より前からいた G のスタックは、その G 自身の今のスタック
func (b *ObservationBuilder) identify(g *GoroutineRecord, ev trace.Event, from trace.GoState, stack StackID, at time.Duration) {
	frames := b.stacks[stack]
	g.Entry = EntryFunction(frames)
	if from == trace.GoNotExist {
		g.Created, g.CreatedAt = true, at
		creator := b.stacks[b.stackOf(ev.Stack())]
		for _, f := range creator {
			if IsAgentFunction(f.Function) {
				g.Agent = true
				break
			}
		}
		for _, f := range creator {
			if !IsRuntimeFunction(f.Function) && !IsWrapperFunction(f.Function) {
				g.Creator = f
				break
			}
		}
	}
	if IsAgentFunction(g.Entry) {
		g.Agent = true
	}
	// GC の worker などは、作るきっかけが agent のコード (trace.Start で走った GC など) でも runtime のもの
	if runtimeWorkers[g.Entry] {
		g.Agent = false
	}
	g.System = !g.Agent && strings.HasPrefix(g.Entry, "runtime.") && g.Entry != "runtime.main"
}

// runtimeWorkers は runtime がいつも持っている goroutine の入口
var runtimeWorkers = map[string]bool{
	"runtime.gcBgMarkWorker": true,
	"runtime.bgsweep":        true,
	"runtime.bgscavenge":     true,
	"runtime.forcegchelper":  true,
	"runtime.runfinq":        true,
	"runtime.runCleanups":    true,
}

// account は goroutine が今の状態にいた時間のうち、まだ集計に足していない分を今の秒に足す。秒の境目でも呼ぶので、閉じた秒を書き換えない
func (b *ObservationBuilder) account(g *goroutineState, at time.Duration) {
	from := max(g.acct, b.cur.From())
	if at <= from {
		return
	}
	d := at - from
	g.acct = at
	s := &b.cur.Stats
	switch {
	case g.rec.App() && g.place == PlaceRunning:
		s.Running += d
		if g.assisting {
			s.MarkAssist += d
		}
	case g.rec.App() && g.cause == CauseLockWait:
		s.LockWait += d
	case g.rec.App() && g.place == PlaceSyscall:
		s.Syscall += d
	case g.rec.Entry == "runtime.gcBgMarkWorker" && g.place == PlaceRunning:
		s.GCWorker += d
	}
}

// TraceDecoder は流れてくる execution trace を 1 つずつイベントにする
type TraceDecoder struct {
	r *trace.Reader
}

func NewTraceDecoder(r io.Reader) (*TraceDecoder, error) {
	tr, err := trace.NewReader(r)
	if err != nil {
		return nil, ErrInvalidArgument.Wrap(err, "open execution trace")
	}
	return &TraceDecoder{r: tr}, nil
}

// Next は trace が終わったら io.EOF を、途中で切れていたらそれ以外のエラーを返す
func (d *TraceDecoder) Next() (trace.Event, error) {
	ev, err := d.r.ReadEvent()
	if errors.Is(err, io.EOF) {
		return trace.Event{}, io.EOF
	}
	if err != nil {
		return trace.Event{}, ErrInvalidArgument.Wrap(err, "read execution trace")
	}
	return ev, nil
}
