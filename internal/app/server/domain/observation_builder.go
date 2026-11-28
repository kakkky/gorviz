package domain

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/trace"
)

// goroutineState は、組み立てている間だけ持つ goroutine の今の状態。acct は、その秒の集計にどこまで足したか
type goroutineState struct {
	rec       *GoroutineRecord
	last      StateChange
	state     trace.GoState
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

// TraceChunk は、アプリの pprof から 1 回で取った trace の区切りと、取り終えたときの OS のスレッドの数
type TraceChunk struct {
	Trace   []byte
	Threads int
}

// ObservationBuilder は、取った順に届く trace の区切りを読み進め、1 秒が閉じるたびに Observation を組んで返す。
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
	waiting   int
	threadNow int
	heapNow   HeapSample

	carried map[GoroutineID]bool
	clock   ClockSync
}

func NewObservationBuilder() *ObservationBuilder {
	b := &ObservationBuilder{
		goroutines: map[GoroutineID]*goroutineState{},
		alive:      map[GoroutineID]*goroutineState{},
		procs:      map[int16]ProcChange{},
		openRanges: map[rangeKey]time.Duration{},
		stacks:     [][]Frame{nil},
		stackKeys:  map[string]StackID{},
		stackCache: map[trace.Stack]StackID{},
		reasons:    []string{""},
		reasonIdx:  map[string]uint16{"": 0},
		threads:    map[trace.ThreadID]int16{},
		maxProc:    -1,
		carried:    map[GoroutineID]bool{},
	}
	b.open(0)
	return b
}

func (b *ObservationBuilder) Started() bool { return b.started }

// Clock は、最後に trace に載った trace の時計と壁時計の組
func (b *ObservationBuilder) Clock() ClockSync { return b.clock }

// open は second 秒目を始める。秒の頭に生きていた goroutine と P の状態を Start・ProcStart に写す
func (b *ObservationBuilder) open(second int) {
	b.cur = Observation{Second: second, Stacks: map[StackID][]Frame{},
		Stats: Bucket{Goroutines: b.appAlive, Runnable: b.runnable, Waiting: b.waiting, Threads: b.threadNow, HeapObjects: b.heapNow.Objects, HeapGoal: b.heapNow.Goal}}
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
	o.Stats.Goroutines, o.Stats.Runnable, o.Stats.Waiting, o.Stats.Threads = b.appAlive, b.runnable, b.waiting, b.threadNow
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

// ApplyChunk は trace の区切り 1 つを足し、それで閉じた秒の Observation を返す。区切りは別々に取った trace なので、
// 2 つ目からは区切りの頭で前の区切りの goroutine を引き継ぎ、終わりで開いている区間を閉じる (次の区切りの頭で開き直される)
func (b *ObservationBuilder) ApplyChunk(events []trace.Event, threads int) []Observation {
	var closed []Observation
	if len(events) > 0 && b.started {
		closed = b.carryOver(events)
	}
	for _, ev := range events {
		closed = append(closed, b.Apply(ev)...)
	}
	b.closeOpenRanges()
	if threads > 0 {
		b.threadNow = threads
	}
	return closed
}

// carryOver は区切りの頭で、前の区切りから続く goroutine を引き継ぐ。区切りには、生きている goroutine の状態が GoUndetermined からの移り変わりとして載る。
// 止まっている goroutine の状態は区切りの終わりにまとめて載るので、区切りを読み切ってから、区切りの頭の状態として当てる。
// 載らなかった goroutine は継ぎ目で終わったものとして、状態が変わっていた goroutine はその状態に、区切りの頭で移す
func (b *ObservationBuilder) carryOver(events []trace.Event) []Observation {
	clear(b.stackCache)
	at := events[0].Time().Sub(b.begin)
	closed := b.advance(at)
	seen := map[GoroutineID]bool{}
	for _, ev := range events {
		if ev.Kind() != trace.EventStateTransition || ev.StateTransition().Resource.Kind != trace.ResourceGoroutine {
			continue
		}
		st := ev.StateTransition()
		id := GoroutineID(st.Resource.Goroutine())
		if seen[id] {
			continue
		}
		seen[id] = true
		from, to := st.Goroutine()
		g, alive := b.alive[id]
		if !alive || from != trace.GoUndetermined {
			continue
		}
		if g.state != to {
			b.goroutineTransition(carriedTransition(id, to, st.Stack, st.Reason), at)
		}
		b.carried[id] = true
	}
	for id := range b.alive {
		if !seen[id] {
			b.goroutineTransition(carriedTransition(id, trace.GoNotExist, trace.NoStack, ""), at)
		}
	}
	return closed
}

func carriedTransition(id GoroutineID, to trace.GoState, stack trace.Stack, reason string) goTransition {
	return goTransition{id: id, from: trace.GoUndetermined, to: to, stack: stack, reason: reason,
		proc: trace.NoProc, thread: trace.NoThread, by: trace.NoGoroutine, byStack: trace.NoStack}
}

func (b *ObservationBuilder) closeOpenRanges() {
	for k, start := range b.openRanges {
		b.setAssisting(k, b.last, false)
		b.closeRange(k, start, b.last)
	}
	clear(b.openRanges)
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
		if c := ev.Sync().ClockSnapshot; c != nil {
			b.clock = ClockSync{At: c.Trace.Sub(b.begin), Wall: c.Wall}
		}
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
			b.goroutineTransition(transitionOf(ev, st), at)
		}
	}
	return closed
}

// End は trace の終わり。開いている区間を閉じ、最後の秒を返す
func (b *ObservationBuilder) End() (Observation, time.Duration) {
	b.closeOpenRanges()
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

func (b *ObservationBuilder) byGorviz(id GoroutineID) bool {
	g, ok := b.goroutines[id]
	return ok && g.rec.Gorviz
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
		// gorviz が trace やプロファイルを取るために起こした STW は、アプリの問題ではないので数えない (再生には出す)
		if b.byGorviz(key.g) {
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

// goTransition は goroutine の状態の移り変わり 1 つ。by は移り変わりを起こした goroutine (作った側・起こした側) と、そのときのスタック。
// 区切りの継ぎ目で gorviz が当てる移り変わりは、P・M・起こした側を持たない
type goTransition struct {
	id      GoroutineID
	from    trace.GoState
	to      trace.GoState
	stack   trace.Stack
	reason  string
	proc    trace.ProcID
	thread  trace.ThreadID
	by      trace.GoID
	byStack trace.Stack
}

func transitionOf(ev trace.Event, st trace.StateTransition) goTransition {
	from, to := st.Goroutine()
	return goTransition{id: GoroutineID(st.Resource.Goroutine()), from: from, to: to, stack: st.Stack, reason: st.Reason,
		proc: ev.Proc(), thread: ev.Thread(), by: ev.Goroutine(), byStack: ev.Stack()}
}

func (b *ObservationBuilder) goroutineTransition(t goTransition, at time.Duration) {
	id, from, to := t.id, t.from, t.to
	if from == to {
		return
	}
	if from == trace.GoUndetermined && b.carried[id] {
		delete(b.carried, id)
		return
	}
	stack := b.stackOf(t.stack)
	g, known := b.goroutines[id]
	if !known {
		g = &goroutineState{rec: &GoroutineRecord{ID: id}}
		b.goroutines[id] = g
		b.identify(g.rec, t, stack, at)
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
	change := StateChange{At: at, G: id, Stack: stack, Proc: -1, Thread: b.threadOf(t.thread)}
	if t.proc != trace.NoProc {
		change.Proc = int16(t.proc)
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
		if from == trace.GoRunning || from == trace.GoSyscall || t.proc == trace.NoProc {
			change.Place = PlaceGlobalQueue
			change.Proc = -1
		}
	case trace.GoWaiting:
		change.Place = PlaceWaiting
		reason := t.reason
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
	if from == trace.GoWaiting && to == trace.GoRunnable && t.by != trace.NoGoroutine && GoroutineID(t.by) != id {
		change.WokenBy = GoroutineID(t.by)
		change.Waker = b.stackOf(t.byStack)
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
		wasWaiting := g.placed && g.place == PlaceWaiting
		nowWaiting := change.Place == PlaceWaiting
		if wasWaiting != nowWaiting {
			if nowWaiting {
				b.waiting++
			} else {
				b.waiting--
			}
		}
	}
	change.Cause = causeIndex(cause)
	b.cur.Changes = append(b.cur.Changes, change)
	g.last, g.state = change, to
	g.place, g.cause, g.since, g.acct, g.placed = change.Place, cause, at, at, true
	if to == trace.GoNotExist {
		g.rec.Ended, g.rec.EndedAt = true, at
		delete(b.alive, id)
		if g.rec.App() {
			b.appAlive--
		}
	}
}

// identify は初めて見た goroutine の素性を決める。作られるのを見た G は生成時のスタックが入口から始まり、byStack は go 文を書いた側のもの。
// trace より前からいた G のスタックは、その G 自身の今のスタック。trace を返している pprof の G は、trace より前からいて、今のスタックに pprof がある
func (b *ObservationBuilder) identify(g *GoroutineRecord, t goTransition, stack StackID, at time.Duration) {
	frames := b.stacks[stack]
	g.Entry = EntryFunction(frames)
	if t.from == trace.GoNotExist {
		g.Created, g.CreatedAt = true, at
		creator := b.stacks[b.stackOf(t.byStack)]
		g.Gorviz = slices.ContainsFunc(creator, func(f Frame) bool { return IsGorvizFunction(f.Function) })
		for _, f := range creator {
			if !IsRuntimeFunction(f.Function) && !IsWrapperFunction(f.Function) {
				g.Creator = f
				break
			}
		}
	} else {
		g.Gorviz = slices.ContainsFunc(frames, func(f Frame) bool { return IsGorvizFunction(f.Function) })
	}
	if IsGorvizFunction(g.Entry) {
		g.Gorviz = true
	}
	// GC の worker などは、作るきっかけが gorviz の処理 (trace を取り始めて走った GC など) でも runtime のもの
	if runtimeWorkers[g.Entry] {
		g.Gorviz = false
	}
	g.System = !g.Gorviz && strings.HasPrefix(g.Entry, "runtime.") && g.Entry != "runtime.main"
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

// ParseThreadCreateProfile は threadcreate プロファイルの先頭の行 ("threadcreate profile: total N") から、OS のスレッドの数を読む。読めなければ 0
func ParseThreadCreateProfile(text []byte) int {
	line, _, _ := bytes.Cut(text, []byte("\n"))
	_, n, ok := bytes.Cut(line, []byte("total "))
	if !ok {
		return 0
	}
	v, _ := strconv.Atoi(string(bytes.TrimSpace(n)))
	return v
}

// DecodeTraceChunk は trace の区切り 1 つをイベントにする。途中で切れていたら、そこまでのイベントとエラーを返す
func DecodeTraceChunk(data []byte) ([]trace.Event, error) {
	r, err := trace.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, ErrInvalidArgument.Wrap(err, "open execution trace")
	}
	var events []trace.Event
	for {
		ev, err := r.ReadEvent()
		if errors.Is(err, io.EOF) {
			return events, nil
		}
		if err != nil {
			return events, ErrInvalidArgument.Wrap(err, "read execution trace")
		}
		events = append(events, ev)
	}
}
