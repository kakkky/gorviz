package domain

import (
	"sort"
	"time"
)

// ObservationSet は、続いた秒の Observation を重ねて、期間について問い合わせられるようにしたもの
type ObservationSet struct {
	observed   Observed
	first      int
	changes    []StateChange
	goroutines map[GoroutineID]*GoroutineRecord
	order      []GoroutineID
	stacks     map[StackID][]Frame
	reasons    []string
	procs      []ProcChange
	maxProc    int
	stw        []TimeRange
	gcMark     []TimeRange
	assists    []TimeRange
	heap       []HeapSample
	buckets    []Bucket
}

// Procs は P の数。trace で見た P の数
func (s *ObservationSet) Procs() int {
	if s.observed.Procs > 0 {
		return s.observed.Procs
	}
	return max(1, s.maxProc)
}

// Last はどこまで観測できたか
func (s *ObservationSet) Last() time.Duration { return s.observed.Until }

// Ended は trace が最後まで届いたか、途中で切れたか
func (s *ObservationSet) Ended() (ended, broken bool) { return s.observed.Ended, s.observed.Broken }

func (s *ObservationSet) Reason(i uint16) string {
	if int(i) < len(s.reasons) {
		return s.reasons[i]
	}
	return ""
}

// ChangesBetween は from から to までの状態の変化の番号の範囲。changes は時刻の順に並んでいる
func (s *ObservationSet) ChangesBetween(from, to time.Duration) (int, int) {
	lo := sort.Search(len(s.changes), func(i int) bool { return s.changes[i].At >= from })
	hi := sort.Search(len(s.changes), func(i int) bool { return s.changes[i].At > to })
	return lo, hi
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
