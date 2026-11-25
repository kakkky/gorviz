package domain

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"time"
)

// 負荷のかけ方は gorviz が決める。phase の長さが決まっているので、診断にかかる時間は毎回ほぼ同じになる
const (
	ReadyTimeout         = 30 * time.Second
	RequestTimeout       = 10 * time.Second
	RampUpLevelDuration  = 5 * time.Second
	CancellationDuration = 5 * time.Second
	CooldownWait         = 3 * time.Second
	CancelShare          = 0.3
	rampUpFactors        = 4
)

// RampUpLevels は ramp-up の level ごとの同時実行数。同時実行数は rps より取り合いに効き、必要な数は P の数で決まるので、P の数から決める
func RampUpLevels(procs int) []int {
	procs = max(1, procs)
	out := make([]int, 0, rampUpFactors)
	for i := range rampUpFactors {
		out = append(out, procs<<i)
	}
	return out
}

func CancellationConcurrency(procs int) int {
	return max(1, procs) * 2
}

// CancelAfter は途中で切るリクエストを切るまでの時間。ふつうに返るまでの時間 (ramp-up のレイテンシの中央値) の 0 倍から 2 倍の間で選ぶ
func CancelAfter(median time.Duration, r float64) time.Duration {
	return max(50*time.Microsecond, time.Duration(float64(2*median)*r))
}

// LoadStats は phase (ramp-up は level) の間に送ったリクエストの結果
type LoadStats struct {
	Phase       string
	Concurrency int
	Duration    time.Duration
	Requests    int
	Latency     Histogram
	Statuses    map[string]int
	Canceled    int
	Failed      int
}

func NewLoadStats(phase string, concurrency int) LoadStats {
	return LoadStats{Phase: phase, Concurrency: concurrency, Statuses: map[string]int{}}
}

func statusKey(r Response) string {
	switch {
	case r.Canceled:
		return "canceled"
	case r.Err != "":
		return "error"
	}
	return strconv.Itoa(r.Status)
}

func (s *LoadStats) Add(r Response, passed bool) {
	s.Requests++
	s.Statuses[statusKey(r)]++
	if r.Canceled {
		s.Canceled++
		return
	}
	s.Latency.Add(r.Latency)
	if !passed {
		s.Failed++
	}
}

func (s *LoadStats) Merge(o LoadStats) {
	s.Requests += o.Requests
	s.Canceled += o.Canceled
	s.Failed += o.Failed
	s.Latency.Merge(&o.Latency)
	for k, v := range o.Statuses {
		s.Statuses[k] += v
	}
}

func (s LoadStats) RPS() float64 {
	if s.Duration <= 0 {
		return 0
	}
	return float64(s.Requests) / s.Duration.Seconds()
}

type StatusCount struct {
	Status string
	Count  int
}

// StatusBreakdown はステータスの内訳。多い順
func (s LoadStats) StatusBreakdown() []StatusCount {
	out := make([]StatusCount, 0, len(s.Statuses))
	for _, k := range slices.Sorted(maps.Keys(s.Statuses)) {
		out = append(out, StatusCount{Status: k, Count: s.Statuses[k]})
	}
	slices.SortStableFunc(out, func(a, b StatusCount) int { return cmp.Compare(b.Count, a.Count) })
	return out
}
