package domain

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

const (
	BaselineStep       = 5 * time.Minute
	baselineWeeks      = 3
	baselineAround     = 30 * time.Minute
	baselineZThreshold = 3.5
	minBaselineWeekN   = 1
)

// ReferenceWindows は at に始まる 5 分と比べる、過去の同じ曜日・同じ時刻の前後 30 分の 5 分ごとの始まり (直近の週から)
func ReferenceWindows(at time.Time) [][]time.Time {
	out := make([][]time.Time, 0, baselineWeeks)
	for w := 1; w <= baselineWeeks; w++ {
		base := at.Add(-time.Duration(w) * 7 * 24 * time.Hour)
		var week []time.Time
		for t := base.Add(-baselineAround); t.Before(base.Add(baselineAround)); t = t.Add(BaselineStep) {
			week = append(week, t)
		}
		out = append(out, week)
	}
	return out
}

func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// ModifiedZScore は 0.6745 × (v − 中央値) ÷ MAD。MAD が 0 なら 1.253314 × 平均絶対偏差を代わりに使い、それも 0 なら判定しない
func ModifiedZScore(v float64, reference []float64) (float64, bool) {
	if len(reference) == 0 {
		return 0, false
	}
	med := median(reference)
	dev := make([]float64, len(reference))
	var sum float64
	for i, r := range reference {
		dev[i] = math.Abs(r - med)
		sum += dev[i]
	}
	mad := median(dev)
	if mad == 0 {
		mad = 1.253314 * sum / float64(len(reference))
	}
	if mad == 0 {
		return 0, false
	}
	return 0.6745 * (v - med) / mad, true
}

type baselineMetric struct {
	kind  IssueKind
	value func(MetricSample) (float64, bool)
	label string
}

var baselineMetrics = []baselineMetric{
	{IssueCPUContention, func(s MetricSample) (float64, bool) {
		d, ok := s.SchedP99()
		return d.Seconds(), ok
	}, "RUNNABLE から RUNNING になるまでの p99 (秒)"},
	{IssueGCPressure, func(s MetricSample) (float64, bool) { return s.GCShare(), s.ProcCapacity > 0 }, "GC の CPU の割合"},
	{IssueLockContention, func(s MetricSample) (float64, bool) { return s.LockWaitRatio(), s.Running > 0 }, "ロックの待ち ÷ 実行中の時間"},
	{IssueThreadExplosion, func(s MetricSample) (float64, bool) { return float64(s.Threads), s.Threads > 0 }, "スレッドの数"},
}

// JudgeByBaseline は 5 分のサービスの値 current を、過去の同じ曜日・時刻の値と比べる。reference は ReferenceWindows の週ごとの 5 分の値。
// 同じ曜日の履歴が 1 週分もそろっていなければ判定しない
func JudgeBaseline(service observation.ServiceName, current MetricSample, reference [][]MetricSample) []Finding {
	var out []Finding
	for _, m := range baselineMetrics {
		v, ok := m.value(current)
		if !ok {
			continue
		}
		var ref []float64
		weeks := 0
		for _, week := range reference {
			n := 0
			for _, s := range week {
				if r, ok := m.value(s); ok {
					ref = append(ref, r)
					n++
				}
			}
			if n > 0 {
				weeks++
			}
		}
		if weeks < minBaselineWeekN {
			continue
		}
		z, ok := ModifiedZScore(v, ref)
		if !ok || z <= baselineZThreshold {
			continue
		}
		out = append(out, Finding{Kind: m.kind, Subject: string(service), Certainty: CertaintyInference, Judge: JudgeByBaseline, Value: v,
			Reason: fmt.Sprintf("%s が普段より大きく外れた (修正 z スコア %.1f > 3.5、%d 週分の %d 個と比較)", m.label, z, weeks, len(ref))})
	}
	return out
}
