package domain

import (
	"cmp"
	"slices"
	"sort"
	"time"
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

// BucketSize は 1 つの Observation の長さ。推移と定期の判定にも使う
const BucketSize = time.Second

// StackID はセッションの中のスタックの番号。0 はスタックがないこと
type StackID uint32

// StateChange は goroutine の状態の変化 1 つ。時刻はセッションの始まり (trace の始まり) からの経過時間。
// Reason は Observation の Reasons の番号。WokenBy は待ちから起こしたのが別の goroutine ならその goroutine、Waker はそのときの起こした側のスタック
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

// GoroutineRecord は 1 つの goroutine の素性。Changes は ObservationSet の中の状態の変化の番号で、Observation の中では空
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

// Bucket は 1 つの Observation の集計。時間は、その秒に入った分だけを足す
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
	Waiting      int
	Threads      int
	HeapObjects  uint64
	HeapGoal     uint64
}

// Observation は、agent から届いた execution trace のうち 1 秒分を整えたもの。その 1 秒だけで答えられるよう、
// 秒の頭に生きていた goroutine と P の状態 (Start・ProcStart)、その秒に出てくる goroutine の素性とスタックを一緒に持つ
type Observation struct {
	Second      int
	Procs       int
	Start       []StateChange
	Changes     []StateChange
	ProcStart   []ProcChange
	ProcChanges []ProcChange
	STW         []TimeRange
	GCMark      []TimeRange
	Assists     []TimeRange
	Heap        []HeapSample
	Marks       []TraceMark
	LeakChecks  map[uint64]time.Duration
	Stats       Bucket
	Goroutines  []GoroutineRecord
	Stacks      map[StackID][]Frame
	Reasons     []string
}

func (o Observation) From() time.Duration { return time.Duration(o.Second) * BucketSize }

func (o Observation) To() time.Duration { return time.Duration(o.Second+1) * BucketSize }

// Observed はセッション全体で観測できたことの要約。どこまで Observation になったか (Until)、trace が終わったか・途中で切れたか、出来事と goroutineleak プロファイルを取った時刻
type Observed struct {
	Started    bool
	Until      time.Duration
	Ended      bool
	Broken     bool
	Procs      int
	Marks      []TraceMark
	LeakChecks map[uint64]time.Duration
	Clock      ClockSync
}

// ClockSync は、trace の上の時刻と、そのときの壁時計の組。server が壁時計で持った出来事を、trace の上に置くため
type ClockSync struct {
	At   time.Duration
	Wall time.Time
}

// TraceAt は壁時計の時刻を、trace の上の時刻に置き換える。trace の時計と壁時計の組がまだ届いていなければ false
func (o Observed) TraceAt(wall time.Time) (time.Duration, bool) {
	if o.Clock.Wall.IsZero() {
		return 0, false
	}
	return max(0, o.Clock.At+wall.Sub(o.Clock.Wall)), true
}

// AddMarks は出来事を時刻の順に足す。trace に書かれた出来事と server が持った出来事が、届く順と時刻の順が違うため
func (o *Observed) AddMarks(marks ...TraceMark) {
	o.Marks = append(slices.Clone(o.Marks), marks...)
	slices.SortStableFunc(o.Marks, func(a, b TraceMark) int { return cmp.Compare(a.At, b.At) })
}

func (o Observed) LeakCheckAt(seq uint64) (time.Duration, bool) {
	at, ok := o.LeakChecks[seq]
	return at, ok
}

// NewObservationSet は、秒の順に続いた Observation を重ねる。期間の頭の状態は、最初の秒の Start から取る
func NewObservationSet(obs []Observation, observed Observed) *ObservationSet {
	s := &ObservationSet{observed: observed, goroutines: map[GoroutineID]*GoroutineRecord{}, stacks: map[StackID][]Frame{}}
	obs = slices.Clone(obs)
	slices.SortFunc(obs, func(a, b Observation) int { return a.Second - b.Second })
	for i, o := range obs {
		if i == 0 {
			s.first = o.Second
			start := slices.Clone(o.Start)
			slices.SortStableFunc(start, func(a, b StateChange) int { return int(a.At - b.At) })
			s.changes = append(s.changes, start...)
			s.procs = append(s.procs, o.ProcStart...)
		}
		s.changes = append(s.changes, o.Changes...)
		s.procs = append(s.procs, o.ProcChanges...)
		for _, g := range o.Goroutines {
			rec := g
			rec.Changes = nil
			if _, seen := s.goroutines[g.ID]; !seen {
				s.order = append(s.order, g.ID)
			}
			s.goroutines[g.ID] = &rec
		}
		for id, f := range o.Stacks {
			s.stacks[id] = f
		}
		if len(o.Reasons) > len(s.reasons) {
			s.reasons = o.Reasons
		}
		s.maxProc = max(s.maxProc, o.Procs)
		s.stw = append(s.stw, o.STW...)
		s.gcMark = append(s.gcMark, o.GCMark...)
		s.assists = append(s.assists, o.Assists...)
		s.heap = append(s.heap, o.Heap...)
		s.buckets = append(s.buckets, o.Stats)
	}
	for i, c := range s.changes {
		if g, ok := s.goroutines[c.G]; ok {
			g.Changes = append(g.Changes, int32(i))
		}
	}
	return s
}

func (s *ObservationSet) Stack(id StackID) []Frame { return s.stacks[id] }

func (s *ObservationSet) Goroutine(id GoroutineID) (*GoroutineRecord, bool) {
	g, ok := s.goroutines[id]
	return g, ok
}

// GoroutineIDs は見た順の goroutine の ID
func (s *ObservationSet) GoroutineIDs() []GoroutineID { return s.order }

// Buckets は from から to にかかる秒の集計
func (s *ObservationSet) Buckets(from, to time.Duration) []Bucket {
	lo := max(0, int(from/BucketSize)-s.first)
	hi := min(len(s.buckets), int((to+BucketSize-1)/BucketSize)-s.first)
	if lo >= hi {
		return nil
	}
	return s.buckets[lo:hi]
}

func rangesIn(rs []TimeRange, from, to time.Duration) []TimeRange {
	var out []TimeRange
	for _, r := range rs {
		if r.End >= from && r.Start <= to {
			out = append(out, r)
		}
	}
	return out
}

func (s *ObservationSet) STW(from, to time.Duration) []TimeRange { return rangesIn(s.stw, from, to) }

// AppSTW は、agent の goroutine が起こしたものを除いた STW
func (s *ObservationSet) AppSTW(from, to time.Duration) []TimeRange {
	var out []TimeRange
	for _, r := range rangesIn(s.stw, from, to) {
		if g, ok := s.goroutines[r.G]; !ok || !g.Agent {
			out = append(out, r)
		}
	}
	return out
}

func (s *ObservationSet) GCMark(from, to time.Duration) []TimeRange {
	return rangesIn(s.gcMark, from, to)
}

func (s *ObservationSet) Assists(from, to time.Duration) []TimeRange {
	return rangesIn(s.assists, from, to)
}

func (s *ObservationSet) Heap() []HeapSample { return s.heap }

func (s *ObservationSet) ChangeAt(i int) StateChange { return s.changes[i] }

func (s *ObservationSet) ProcChanges(from, to time.Duration) (before []ProcChange, within []ProcChange) {
	last := map[int16]ProcChange{}
	for _, p := range s.procs {
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
func (s *ObservationSet) StateAt(g *GoroutineRecord, at time.Duration) (StateChange, bool) {
	i := sort.Search(len(g.Changes), func(i int) bool { return s.changes[g.Changes[i]].At > at })
	if i == 0 {
		return StateChange{}, false
	}
	return s.changes[g.Changes[i-1]], true
}

// Intervals は goroutine が from から to までにいた状態の区間。期間の外にはみ出した分は切る
func (s *ObservationSet) Intervals(g *GoroutineRecord, from, to time.Duration) []Interval {
	end := min(to, s.Last())
	start := sort.Search(len(g.Changes), func(i int) bool { return s.changes[g.Changes[i]].At > from })
	if start > 0 {
		start--
	}
	var out []Interval
	for i := start; i < len(g.Changes); i++ {
		c := s.changes[g.Changes[i]]
		if c.At > end || c.Place == PlaceDead {
			break
		}
		iv := Interval{Start: max(c.At, from), End: end, Change: c}
		if i+1 < len(g.Changes) {
			if n := s.changes[g.Changes[i+1]]; n.At < end {
				iv.End, iv.Next, iv.HasNext = n.At, n, true
			}
		}
		if iv.End > iv.Start {
			out = append(out, iv)
		}
	}
	return out
}
