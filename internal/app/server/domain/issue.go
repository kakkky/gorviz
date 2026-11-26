package domain

import (
	"fmt"
	"slices"
	"strconv"
	"time"
)

type IssueKind string

const (
	IssueGoroutineLeak   IssueKind = "GOROUTINE_LEAK"
	IssueCPUContention   IssueKind = "CPU_CONTENTION"
	IssueGCPressure      IssueKind = "GC_PRESSURE"
	IssueLockContention  IssueKind = "LOCK_CONTENTION"
	IssueLongSTW         IssueKind = "LONG_STW"
	IssueThreadExplosion IssueKind = "THREAD_EXPLOSION"
)

var IssueKinds = []IssueKind{IssueGoroutineLeak, IssueCPUContention, IssueGCPressure, IssueLockContention, IssueLongSTW, IssueThreadExplosion}

// NextAction は、どこを見直せばよいかの方向。直し方そのものは示さない
func (k IssueKind) NextAction() string {
	switch k {
	case IssueGoroutineLeak:
		return "Look at the go statement that created the goroutines and where they are blocked, and check that they have a way to finish (context cancellation, closing the channel, a timeout)."
	case IssueCPUContention:
		return "Review the work of the goroutines that used the CPU for long in the same period."
	case IssueGCPressure:
		return "Reduce the allocations of the goroutines with long mark assist, or review GOGC and GOMEMLIMIT."
	case IssueLockContention:
		return "Look at the functions that called Lock with the most waiting, and review the granularity of the lock, how long it is held and how it is split."
	case IssueLongSTW:
		return "Look at the reason of the stop-the-world and review the calls that cause it (runtime.ReadMemStats, runtime.GC, taking profiles and so on)."
	case IssueThreadExplosion:
		return "Look at where the syscalls and cgo calls are made, and limit how many of them run at the same time."
	}
	return ""
}

type Certainty string

const (
	CertaintyFact      Certainty = "FACT"
	CertaintyInference Certainty = "INFERENCE"
)

func stronger(a, b Certainty) Certainty {
	if a == CertaintyFact || b == CertaintyFact {
		return CertaintyFact
	}
	return CertaintyInference
}

type IssueID string

// LeakStack はリークした goroutine を同じ場所で待っているものごとにまとめたもの。Of は同じ場所で待っていた goroutine の数
type LeakStack struct {
	Creator    Frame
	Blocked    Frame
	Reason     string
	Count      int
	Of         int
	Goroutines []GoroutineID
	Certainty  Certainty
}

// Detection は判定 1 回で見つかった問題
type Detection struct {
	Kind      IssueKind
	Target    string
	Certainty Certainty
	From      time.Duration
	To        time.Duration
	Value     string
	Leaks     []LeakStack
}

// Occurrence は同じ Issue が条件を満たし始めてから満たさなくなるまでの 1 回
type Occurrence struct {
	From      time.Duration
	To        time.Duration
	Phase     string
	Certainty Certainty
	Value     string
	Leaks     []LeakStack
}

type Issue struct {
	ID          IssueID
	Kind        IssueKind
	Target      string
	Occurrences []Occurrence
}

func (i *Issue) Certainty() Certainty {
	c := CertaintyInference
	for _, o := range i.Occurrences {
		c = stronger(c, o.Certainty)
	}
	return c
}

// FirstPhase は発生のうち最も早いものがかかった phase
func (i *Issue) FirstPhase() string {
	for _, o := range i.Occurrences {
		if o.Phase != "" {
			return o.Phase
		}
	}
	return ""
}

func (i *Issue) Occurrence(n int) (Occurrence, bool) {
	if n < 1 || n > len(i.Occurrences) {
		return Occurrence{}, false
	}
	return i.Occurrences[n-1], true
}

// TargetLabel は Issue の対象。アプリ全体で判定するものは "app"
func (i *Issue) TargetLabel() string {
	if i.Target == "" {
		return "app"
	}
	return i.Target
}

type issueKey struct {
	kind   IssueKind
	target string
}

// IssueBook はセッションで見つけた Issue。解決の状態は持たない
type IssueBook struct {
	issues        []*Issue
	byKey         map[issueKey]*Issue
	DetectedUntil time.Duration
	Pending       []PendingLeakCheck
}

// PendingLeakCheck は届いた goroutineleak プロファイルのうち、取った時刻の trace がまだ index に届いていないもの
type PendingLeakCheck struct {
	Seq    uint64
	Stacks []LeakProfileStack
}

func NewIssueBook() *IssueBook {
	return &IssueBook{byKey: map[issueKey]*Issue{}}
}

func (b *IssueBook) issueOf(d Detection) *Issue {
	k := issueKey{kind: d.Kind, target: d.Target}
	is, ok := b.byKey[k]
	if !ok {
		is = &Issue{ID: IssueID("I" + strconv.Itoa(len(b.issues)+1)), Kind: d.Kind, Target: d.Target}
		b.byKey[k] = is
		b.issues = append(b.issues, is)
	}
	return is
}

// Record は定期の判定で見つけた問題を足す。前の発生から間を空けずに続いていれば、同じ発生を延ばす
func (b *IssueBook) Record(d Detection, phase string) {
	is := b.issueOf(d)
	if n := len(is.Occurrences); n > 0 {
		last := &is.Occurrences[n-1]
		if d.From <= last.To+BucketSize && d.From >= last.From {
			last.To = max(last.To, d.To)
			last.Certainty = stronger(last.Certainty, d.Certainty)
			last.Value = d.Value
			if d.Leaks != nil {
				last.Leaks = d.Leaks
			}
			if last.Phase == "" {
				last.Phase = phase
			}
			return
		}
	}
	is.Occurrences = append(is.Occurrences, Occurrence{From: d.From, To: d.To, Phase: phase, Certainty: d.Certainty, Value: d.Value, Leaks: d.Leaks})
	sortOccurrences(is)
}

// RecordPhase は phase ごとの判定で見つけた問題を足す。phase にかかる発生がすでにあれば、その発生に phase を添える
func (b *IssueBook) RecordPhase(d Detection, phase string) IssueID {
	is := b.issueOf(d)
	for i := range is.Occurrences {
		o := &is.Occurrences[i]
		if o.From <= d.To && d.From <= o.To {
			if o.Phase == "" {
				o.Phase = phase
			}
			o.Certainty = stronger(o.Certainty, d.Certainty)
			if d.Leaks != nil {
				o.Leaks = d.Leaks
			}
			return is.ID
		}
	}
	is.Occurrences = append(is.Occurrences, Occurrence{From: d.From, To: d.To, Phase: phase, Certainty: d.Certainty, Value: d.Value, Leaks: d.Leaks})
	sortOccurrences(is)
	return is.ID
}

func sortOccurrences(is *Issue) {
	slices.SortStableFunc(is.Occurrences, func(a, b Occurrence) int {
		switch {
		case a.From < b.From:
			return -1
		case a.From > b.From:
			return 1
		}
		return 0
	})
}

// Clone は、書き換えても元に響かない写し
func (b *IssueBook) Clone() *IssueBook {
	out := &IssueBook{byKey: make(map[issueKey]*Issue, len(b.byKey)), DetectedUntil: b.DetectedUntil, Pending: slices.Clone(b.Pending)}
	for _, is := range b.issues {
		c := *is
		c.Occurrences = slices.Clone(is.Occurrences)
		out.issues = append(out.issues, &c)
		out.byKey[issueKey{kind: c.Kind, target: c.Target}] = &c
	}
	return out
}

func (b *IssueBook) Issues() []*Issue { return b.issues }

func (b *IssueBook) Issue(id IssueID) (*Issue, bool) {
	for _, is := range b.issues {
		if is.ID == id {
			return is, true
		}
	}
	return nil, false
}

// IssuesIn は from から to にかかる発生のある Issue
func (b *IssueBook) IssuesIn(from, to time.Duration) []*Issue {
	var out []*Issue
	for _, is := range b.issues {
		for _, o := range is.Occurrences {
			if o.From <= to && from <= o.To {
				out = append(out, is)
				break
			}
		}
	}
	return out
}

// IssueNotFound は見つからない ID に、あるものを添えたエラー
func IssueNotFound(id IssueID, issues []*Issue) error {
	ids := make([]string, 0, len(issues))
	for _, is := range issues {
		ids = append(ids, string(is.ID))
	}
	if len(ids) == 0 {
		return ErrNotFound.With(fmt.Sprintf("issue %s does not exist; no issue has been found in this session yet", id))
	}
	return ErrNotFound.With(fmt.Sprintf("issue %s does not exist; the issues are %v", id, ids))
}
