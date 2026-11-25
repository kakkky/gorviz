package domain

import (
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/trace"

	"github.com/kakkky/gorviz/internal/protocol/agentsocket"
)

type GoroutineID int64

// PlaceKind は GMP の概念図の上の置き場所。並びは GUI に渡す番号でもある
type PlaceKind uint8

const (
	PlaceRunning PlaceKind = iota
	PlaceLocalQueue
	PlaceGlobalQueue
	PlaceWaiting
	PlaceSyscall
	PlaceDead
)

var PlaceKindNames = []string{"running", "local_run_queue", "global_run_queue", "waiting", "syscall", "dead"}

// BucketSize は推移と定期の判定に使う時間幅
const BucketSize = time.Second

// StackID は index の中のスタックの番号。0 はスタックがないこと
type StackID uint32

// StateChange は goroutine の状態の変化 1 つ。時刻はセッションの始まり (trace の始まり) からの経過時間。
// WokenBy は待ちから起こしたのが別の goroutine ならその goroutine、Waker はそのときの起こした側のスタック
type StateChange struct {
	At       time.Duration
	G        GoroutineID
	WokenBy  GoroutineID
	Stack    StackID
	Waker    StackID
	Reason   uint16
	Proc     int16
	Thread   int16
	Place    PlaceKind
	Cause    uint8
	Inferred bool
}

func (c StateChange) MainCause() (MainCause, bool) {
	if c.Cause == 0 {
		return "", false
	}
	return MainCauses[c.Cause-1], true
}

func causeIndex(c MainCause) uint8 {
	for i, m := range MainCauses {
		if m == c {
			return uint8(i + 1)
		}
	}
	return 0
}

// GoroutineRecord は 1 つの goroutine の素性と、状態の変化の並び (index の changes の番号)
type GoroutineRecord struct {
	ID        GoroutineID
	Entry     string
	Creator   Frame
	CreatedAt time.Duration
	Created   bool
	Agent     bool
	System    bool
	Ended     bool
	EndedAt   time.Duration
	Changes   []int32

	place  PlaceKind
	cause  MainCause
	since  time.Duration
	placed bool
}

// App はアプリの goroutine か。agent と runtime の goroutine は Issue にも推移にも数えない
func (g *GoroutineRecord) App() bool {
	return !g.Agent && !g.System
}

type ProcChange struct {
	At      time.Duration
	Proc    int16
	Running bool
	Thread  int16
}

type TimeRange struct {
	Start time.Duration
	End   time.Duration
	G     GoroutineID
	// Reason は STW の理由
	Reason string
}

type HeapSample struct {
	At      time.Duration
	Objects uint64
	Goal    uint64
}

// TraceMark は agent が server に言われて trace に書いた出来事
type TraceMark struct {
	At      time.Duration
	Message string
}

// Bucket は BucketSize ごとの値。時間は、その幅に入った分だけを足す
type Bucket struct {
	RunnableWait Histogram
	Running      time.Duration
	LockWait     time.Duration
	Syscall      time.Duration
	GCWorker     time.Duration
	MarkAssist   time.Duration
	STW          time.Duration
	STWMax       time.Duration
	Goroutines   int
	Runnable     int
	Threads      int
	HeapObjects  uint64
	HeapGoal     uint64
}

type rangeKey struct {
	name string
	g    GoroutineID
}

// TraceIndex は agent から届いた execution trace を整形した、観測データの index。
// 届いた順に Apply で足し、Issue の判定・goroutine の詳細・スケジューラの動きの再生はここから引く
type TraceIndex struct {
	begin   trace.Time
	started bool
	last    time.Duration
	ended   bool
	broken  bool
	procs   int

	changes    []StateChange
	goroutines map[GoroutineID]*GoroutineRecord
	order      []GoroutineID

	stacks     [][]Frame
	stackKeys  map[string]StackID
	stackCache map[trace.Stack]StackID
	reasons    []string
	reasonIdx  map[string]uint16
	threads    map[trace.ThreadID]int16

	procChanges []ProcChange
	maxProc     int16
	stw         []TimeRange
	gcMark      []TimeRange
	assists     []TimeRange
	heap        []HeapSample
	marks       []TraceMark
	leakChecks  map[uint64]time.Duration
	openRanges  map[rangeKey]time.Duration

	buckets   []Bucket
	appAlive  int
	runnable  int
	threadNow int
	heapNow   HeapSample
}

func NewTraceIndex() *TraceIndex {
	return &TraceIndex{
		goroutines: map[GoroutineID]*GoroutineRecord{},
		stacks:     [][]Frame{nil},
		stackKeys:  map[string]StackID{},
		stackCache: map[trace.Stack]StackID{},
		reasons:    []string{""},
		reasonIdx:  map[string]uint16{"": 0},
		threads:    map[trace.ThreadID]int16{},
		leakChecks: map[uint64]time.Duration{},
		openRanges: map[rangeKey]time.Duration{},
		maxProc:    -1,
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

func (ix *TraceIndex) SetProcs(n int) {
	if n > 0 {
		ix.procs = n
	}
}

// Procs は P の数。agent が伝えた数がなければ、trace で見た P の数
func (ix *TraceIndex) Procs() int {
	if ix.procs > 0 {
		return ix.procs
	}
	return max(1, int(ix.maxProc)+1)
}

func (ix *TraceIndex) Last() time.Duration { return ix.last }

func (ix *TraceIndex) Started() bool { return ix.started }

// End は trace が最後まで届いたこと。Broken は途中で切れたこと (アプリがその場で終わったなど)
func (ix *TraceIndex) End(broken bool) {
	ix.ended = true
	ix.broken = broken
	for k, start := range ix.openRanges {
		ix.closeRange(k, start, ix.last)
	}
	ix.openRanges = map[rangeKey]time.Duration{}
	for _, id := range ix.order {
		g := ix.goroutines[id]
		if g.placed && !g.Ended {
			ix.account(g, ix.last)
		}
	}
}

func (ix *TraceIndex) Ended() (ended, broken bool) { return ix.ended, ix.broken }

func (ix *TraceIndex) Apply(ev trace.Event) {
	if !ix.started {
		ix.begin, ix.started = ev.Time(), true
	}
	at := ev.Time().Sub(ix.begin)
	if at > ix.last {
		ix.last = at
	}
	switch ev.Kind() {
	case trace.EventSync:
		clear(ix.stackCache)
	case trace.EventLog:
		ix.log(ev, at)
	case trace.EventRangeBegin, trace.EventRangeActive, trace.EventRangeEnd:
		ix.rangeEvent(ev, at)
	case trace.EventMetric:
		ix.metric(ev, at)
	case trace.EventStateTransition:
		st := ev.StateTransition()
		switch st.Resource.Kind {
		case trace.ResourceProc:
			ix.procTransition(ev, st, at)
		case trace.ResourceGoroutine:
			ix.goroutineTransition(ev, st, at)
		}
	}
}

func (ix *TraceIndex) bucket(at time.Duration) *Bucket {
	i := int(at / BucketSize)
	for len(ix.buckets) <= i {
		ix.buckets = append(ix.buckets, Bucket{Goroutines: ix.appAlive, Runnable: ix.runnable, Threads: ix.threadNow, HeapObjects: ix.heapNow.Objects, HeapGoal: ix.heapNow.Goal})
	}
	return &ix.buckets[i]
}

// spread は区間の長さを、区間がかかる時間幅に分けて足す
func (ix *TraceIndex) spread(from, to time.Duration, add func(b *Bucket, d time.Duration)) {
	for from < to {
		end := min(to, (from/BucketSize+1)*BucketSize)
		add(ix.bucket(from), end-from)
		from = end
	}
}

func (ix *TraceIndex) threadOf(th trace.ThreadID) int16 {
	if th == trace.NoThread {
		return -1
	}
	id, ok := ix.threads[th]
	if !ok {
		id = int16(len(ix.threads))
		ix.threads[th] = id
	}
	return id
}

func (ix *TraceIndex) reasonOf(r string) uint16 {
	if i, ok := ix.reasonIdx[r]; ok {
		return i
	}
	i := uint16(len(ix.reasons))
	ix.reasons = append(ix.reasons, r)
	ix.reasonIdx[r] = i
	return i
}

func (ix *TraceIndex) Reason(i uint16) string { return ix.reasons[i] }

func (ix *TraceIndex) stackOf(s trace.Stack) StackID {
	if s == trace.NoStack {
		return 0
	}
	if id, ok := ix.stackCache[s]; ok {
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
	id, ok := ix.stackKeys[key.String()]
	if !ok {
		id = StackID(len(ix.stacks))
		ix.stacks = append(ix.stacks, frames)
		ix.stackKeys[key.String()] = id
	}
	ix.stackCache[s] = id
	return id
}

func (ix *TraceIndex) Stack(id StackID) []Frame { return ix.stacks[id] }

func (ix *TraceIndex) log(ev trace.Event, at time.Duration) {
	l := ev.Log()
	if l.Category != agentsocket.TraceCategory {
		return
	}
	switch {
	case strings.HasPrefix(l.Message, agentsocket.EventThreadCountPrefix):
		if n, err := strconv.Atoi(strings.TrimPrefix(l.Message, agentsocket.EventThreadCountPrefix)); err == nil {
			ix.threadNow = n
			ix.bucket(at).Threads = n
		}
	case strings.HasPrefix(l.Message, agentsocket.EventLeakCheckPrefix):
		if n, err := strconv.ParseUint(strings.TrimPrefix(l.Message, agentsocket.EventLeakCheckPrefix), 10, 64); err == nil {
			ix.leakChecks[n] = at
		}
	default:
		ix.marks = append(ix.marks, TraceMark{At: at, Message: l.Message})
	}
}

func (ix *TraceIndex) LeakCheckAt(seq uint64) (time.Duration, bool) {
	at, ok := ix.leakChecks[seq]
	return at, ok
}

func (ix *TraceIndex) Marks() []TraceMark { return ix.marks }

func (ix *TraceIndex) metric(ev trace.Event, at time.Duration) {
	m := ev.Metric()
	switch m.Name {
	case "/memory/classes/heap/objects:bytes":
		ix.heapNow.Objects = m.Value.Uint64()
	case "/gc/heap/goal:bytes":
		ix.heapNow.Goal = m.Value.Uint64()
	default:
		return
	}
	ix.heapNow.At = at
	ix.heap = append(ix.heap, ix.heapNow)
	b := ix.bucket(at)
	b.HeapObjects, b.HeapGoal = ix.heapNow.Objects, ix.heapNow.Goal
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

func (ix *TraceIndex) rangeEvent(ev trace.Event, at time.Duration) {
	key, ok := rangeKeyOf(ev)
	if !ok {
		return
	}
	switch ev.Kind() {
	case trace.EventRangeBegin:
		ix.openRanges[key] = at
	case trace.EventRangeActive:
		if _, open := ix.openRanges[key]; !open {
			ix.openRanges[key] = at
		}
	case trace.EventRangeEnd:
		start, open := ix.openRanges[key]
		if !open {
			return
		}
		delete(ix.openRanges, key)
		if key.name == "" {
			return
		}
		if strings.HasPrefix(key.name, "stop-the-world") {
			key.g = GoroutineID(ev.Goroutine())
		}
		ix.closeRange(key, start, at)
	}
}

func (ix *TraceIndex) closeRange(key rangeKey, start, end time.Duration) {
	switch {
	case key.name == "GC concurrent mark phase":
		ix.gcMark = append(ix.gcMark, TimeRange{Start: start, End: end})
	case key.name == "GC mark assist":
		ix.assists = append(ix.assists, TimeRange{Start: start, End: end, G: key.g})
		// mark assist の区間は、assist の順番を待って止まっている間も続くので、走っていた分だけを CPU として数える
		if g, ok := ix.goroutines[key.g]; ok && g.App() {
			for _, iv := range ix.Intervals(g, start, end) {
				if iv.Change.Place == PlaceRunning {
					ix.spread(iv.Start, iv.End, func(b *Bucket, d time.Duration) { b.MarkAssist += d })
				}
			}
		}
	case strings.HasPrefix(key.name, "stop-the-world"):
		reason := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(key.name, "stop-the-world"), " ("), ")")
		ix.stw = append(ix.stw, TimeRange{Start: start, End: end, G: key.g, Reason: reason})
		// agent が goroutineleak プロファイルのために走らせた GC などの STW は、アプリの問題ではないので数えない (再生には出す)
		if ix.byAgent(key.g) {
			return
		}
		ix.spread(start, end, func(b *Bucket, d time.Duration) { b.STW += d })
		b := ix.bucket(start)
		b.STWMax = max(b.STWMax, end-start)
	}
}

func (ix *TraceIndex) procTransition(ev trace.Event, st trace.StateTransition, at time.Duration) {
	p := int16(st.Resource.Proc())
	ix.maxProc = max(ix.maxProc, p)
	_, to := st.Proc()
	switch to {
	case trace.ProcRunning:
		ix.procChanges = append(ix.procChanges, ProcChange{At: at, Proc: p, Running: true, Thread: ix.threadOf(ev.Thread())})
	case trace.ProcIdle:
		ix.procChanges = append(ix.procChanges, ProcChange{At: at, Proc: p, Thread: -1})
	}
}

func (ix *TraceIndex) record(id GoroutineID) *GoroutineRecord {
	g, ok := ix.goroutines[id]
	if !ok {
		g = &GoroutineRecord{ID: id}
		ix.goroutines[id] = g
		ix.order = append(ix.order, id)
	}
	return g
}

func (ix *TraceIndex) goroutineTransition(ev trace.Event, st trace.StateTransition, at time.Duration) {
	id := GoroutineID(st.Resource.Goroutine())
	from, to := st.Goroutine()
	if from == to {
		return
	}
	stack := ix.stackOf(st.Stack)
	g, known := ix.goroutines[id]
	if !known {
		g = ix.record(id)
		ix.identify(g, ev, from, stack, at)
	} else if IsWrapperFunction(g.Entry) && stack != 0 {
		// go 文の関数がラッパー (scope など) なら、生成時のスタックにはラッパーしか載らないので、後でスタックが載ったときに入口を決め直す
		g.Entry = EntryFunction(ix.stacks[stack])
	}
	if from == trace.GoNotExist || from == trace.GoUndetermined {
		if g.App() && !g.placed {
			ix.appAlive++
			ix.bucket(at).Goroutines = ix.appAlive
		}
	}
	change := StateChange{At: at, G: id, Stack: stack, Proc: -1, Thread: ix.threadOf(ev.Thread())}
	if ev.Proc() != trace.NoProc {
		change.Proc = int16(ev.Proc())
		ix.maxProc = max(ix.maxProc, change.Proc)
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
		frames := ix.stacks[stack]
		if from == trace.GoUndetermined && reason == "" {
			reason = InferWaitReason(frames)
			change.Inferred = true
		}
		if reason == "" {
			reason = "unknown"
		}
		change.Reason = ix.reasonOf(reason)
		cause = CauseOfWait(reason, frames)
	case trace.GoSyscall:
		change.Place, cause = PlaceSyscall, CauseIOWait
		if isCgoStack(ix.stacks[stack]) {
			cause = CauseCgo
		}
	case trace.GoNotExist:
		change.Place = PlaceDead
	default:
		return
	}
	if from == trace.GoWaiting && to == trace.GoRunnable && ev.Goroutine() != trace.NoGoroutine && GoroutineID(ev.Goroutine()) != id {
		change.WokenBy = GoroutineID(ev.Goroutine())
		change.Waker = ix.stackOf(ev.Stack())
	}
	if g.App() {
		if g.placed {
			ix.account(g, at)
		}
		if to == trace.GoRunning && g.placed && (g.place == PlaceLocalQueue || g.place == PlaceGlobalQueue) {
			ix.bucket(at).RunnableWait.Add(at - g.since)
		}
		wasRunnable := g.placed && (g.place == PlaceLocalQueue || g.place == PlaceGlobalQueue)
		nowRunnable := change.Place == PlaceLocalQueue || change.Place == PlaceGlobalQueue
		if wasRunnable != nowRunnable {
			if nowRunnable {
				ix.runnable++
			} else {
				ix.runnable--
			}
			ix.bucket(at).Runnable = ix.runnable
		}
	} else if g.Entry == "runtime.gcBgMarkWorker" && g.placed && g.place == PlaceRunning {
		ix.spread(g.since, at, func(b *Bucket, d time.Duration) { b.GCWorker += d })
	}
	change.Cause = causeIndex(cause)
	g.Changes = append(g.Changes, int32(len(ix.changes)))
	ix.changes = append(ix.changes, change)
	g.place, g.cause, g.since, g.placed = change.Place, cause, at, true
	if to == trace.GoNotExist {
		g.Ended, g.EndedAt = true, at
		if g.App() {
			ix.appAlive--
			ix.bucket(at).Goroutines = ix.appAlive
		}
	}
}

// identify は初めて見た goroutine の素性を決める。作られるのを見た G は生成時のスタックが入口から始まり、イベントのスタックは go 文を書いた側のもの。
// trace より前からいた G のスタックは、その G 自身の今のスタック
func (ix *TraceIndex) identify(g *GoroutineRecord, ev trace.Event, from trace.GoState, stack StackID, at time.Duration) {
	frames := ix.stacks[stack]
	g.Entry = EntryFunction(frames)
	if from == trace.GoNotExist {
		g.Created, g.CreatedAt = true, at
		creator := ix.stacks[ix.stackOf(ev.Stack())]
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

// account は app の goroutine が今の状態にいた時間を、時間幅ごとに足す
func (ix *TraceIndex) account(g *GoroutineRecord, at time.Duration) {
	switch {
	case g.place == PlaceRunning:
		ix.spread(g.since, at, func(b *Bucket, d time.Duration) { b.Running += d })
	case g.cause == CauseLockWait:
		ix.spread(g.since, at, func(b *Bucket, d time.Duration) { b.LockWait += d })
	case g.place == PlaceSyscall:
		ix.spread(g.since, at, func(b *Bucket, d time.Duration) { b.Syscall += d })
	}
}

func (ix *TraceIndex) Goroutine(id GoroutineID) (*GoroutineRecord, bool) {
	g, ok := ix.goroutines[id]
	return g, ok
}

// GoroutineIDs は見た順の goroutine の ID
func (ix *TraceIndex) GoroutineIDs() []GoroutineID { return ix.order }

func (ix *TraceIndex) Change(i int32) StateChange { return ix.changes[i] }

// Buckets は from から to にかかる時間幅
func (ix *TraceIndex) Buckets(from, to time.Duration) []Bucket {
	lo := max(0, int(from/BucketSize))
	hi := min(len(ix.buckets), int((to+BucketSize-1)/BucketSize))
	if lo >= hi {
		return nil
	}
	return ix.buckets[lo:hi]
}

func (ix *TraceIndex) BucketCount() int { return len(ix.buckets) }

func rangesIn(rs []TimeRange, from, to time.Duration) []TimeRange {
	var out []TimeRange
	for _, r := range rs {
		if r.End >= from && r.Start <= to {
			out = append(out, r)
		}
	}
	return out
}

func (ix *TraceIndex) STW(from, to time.Duration) []TimeRange { return rangesIn(ix.stw, from, to) }

// AppSTW は、agent の goroutine が起こしたものを除いた STW
func (ix *TraceIndex) AppSTW(from, to time.Duration) []TimeRange {
	var out []TimeRange
	for _, r := range rangesIn(ix.stw, from, to) {
		if !ix.byAgent(r.G) {
			out = append(out, r)
		}
	}
	return out
}

func (ix *TraceIndex) byAgent(g GoroutineID) bool {
	rec, ok := ix.goroutines[g]
	return ok && rec.Agent
}

func (ix *TraceIndex) GCMark(from, to time.Duration) []TimeRange {
	return rangesIn(ix.gcMark, from, to)
}

func (ix *TraceIndex) Assists(from, to time.Duration) []TimeRange {
	return rangesIn(ix.assists, from, to)
}

func (ix *TraceIndex) Heap() []HeapSample { return ix.heap }

// ChangesBetween は from から to までの状態の変化の番号の範囲。changes は時刻の順に並んでいる
func (ix *TraceIndex) ChangesBetween(from, to time.Duration) (int, int) {
	lo := sort.Search(len(ix.changes), func(i int) bool { return ix.changes[i].At >= from })
	hi := sort.Search(len(ix.changes), func(i int) bool { return ix.changes[i].At > to })
	return lo, hi
}

func (ix *TraceIndex) ChangeAt(i int) StateChange { return ix.changes[i] }

func (ix *TraceIndex) ProcChanges(from, to time.Duration) (before []ProcChange, within []ProcChange) {
	last := map[int16]ProcChange{}
	for _, p := range ix.procChanges {
		switch {
		case p.At < from:
			last[p.Proc] = p
		case p.At <= to:
			within = append(within, p)
		}
	}
	for _, p := range last {
		before = append(before, p)
	}
	sort.Slice(before, func(i, j int) bool { return before[i].Proc < before[j].Proc })
	return before, within
}

// StateAt は goroutine が at の時点でいた状態の変化。まだいなければ false
func (ix *TraceIndex) StateAt(g *GoroutineRecord, at time.Duration) (StateChange, bool) {
	i := sort.Search(len(g.Changes), func(i int) bool { return ix.changes[g.Changes[i]].At > at })
	if i == 0 {
		return StateChange{}, false
	}
	return ix.changes[g.Changes[i-1]], true
}

// Interval は goroutine が 1 つの状態にいた区間
type Interval struct {
	Start  time.Duration
	End    time.Duration
	Change StateChange
	// Next は区間を終わらせた変化。ないときは期間の終わりで切った
	Next    StateChange
	HasNext bool
}

// Intervals は goroutine が from から to までにいた状態の区間。期間の外にはみ出した分は切る
func (ix *TraceIndex) Intervals(g *GoroutineRecord, from, to time.Duration) []Interval {
	end := min(to, ix.last)
	start := sort.Search(len(g.Changes), func(i int) bool { return ix.changes[g.Changes[i]].At > from })
	if start > 0 {
		start--
	}
	var out []Interval
	for i := start; i < len(g.Changes); i++ {
		c := ix.changes[g.Changes[i]]
		if c.At > end {
			break
		}
		if c.Place == PlaceDead {
			break
		}
		iv := Interval{Start: max(c.At, from), End: end, Change: c}
		if i+1 < len(g.Changes) {
			n := ix.changes[g.Changes[i+1]]
			if n.At < end {
				iv.End, iv.Next, iv.HasNext = n.At, n, true
			}
		}
		if iv.End > iv.Start {
			out = append(out, iv)
		}
	}
	return out
}
