package domain

import (
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/pkg/histogram"
)

const MetricStep = 10 * time.Second

// MetricSample は 1 つのインスタンスの 10 秒分。どの値も足し合わせられる形 (合計・最大・分布) で持ち、
// サービスの値や長い区切りの値は、インスタンスと区切りをまたいで Merge して出す
type MetricSample struct {
	At            time.Time                `json:"at"`
	Instance      observation.InstanceName `json:"instance,omitempty"`
	Goroutines    int                      `json:"goroutines"`
	HasGoroutines bool                     `json:"hasGoroutines"`
	Logs          int                      `json:"logs"`
	Errors        int                      `json:"errors"`
	HeapBytes     uint64                   `json:"heapBytes"`
	HeapGoalBytes uint64                   `json:"heapGoalBytes"`
	Threads       int                      `json:"threads"`
	Traced        time.Duration            `json:"traced"`
	ProcCapacity  time.Duration            `json:"procCapacity"`
	Running       time.Duration            `json:"running"`
	Runnable      time.Duration            `json:"runnable"`
	GC            time.Duration            `json:"gc"`
	MarkAssist    time.Duration            `json:"markAssist"`
	LockWait      time.Duration            `json:"lockWait"`
	SchedLatency  histogram.Histogram      `json:"schedLatency"`
	STW           histogram.Histogram      `json:"stw"`
}

// ChunkSample はチャンク 1 つ分の値
func ChunkSample(c observation.TraceChunk) MetricSample {
	s := MetricSample{
		Threads:      c.Threads,
		Traced:       c.Duration,
		ProcCapacity: c.Duration * time.Duration(max(c.Stats.Procs, 1)),
		Running:      c.Stats.ProcRunning,
		Runnable:     c.Stats.Runnable,
		GC:           c.Stats.ProcGC,
		SchedLatency: c.Stats.SchedLatency,
		STW:          c.Stats.STW,
	}
	s.MarkAssist = markAssistOnProcs(c)
	for _, w := range c.Stats.Waits {
		if w.Kind == observation.PlaceWaiting && IsLockFunction(w.Function) {
			s.LockWait += w.Total
		}
	}
	for _, h := range c.Heap {
		s.HeapBytes, s.HeapGoalBytes = max(s.HeapBytes, h.ObjectsBytes), max(s.HeapGoalBytes, h.GoalBytes)
	}
	return s
}

func (s *MetricSample) Merge(o MetricSample) {
	if o.HasGoroutines {
		s.Goroutines += o.Goroutines
		s.HasGoroutines = true
	}
	s.Logs += o.Logs
	s.Errors += o.Errors
	s.HeapBytes += o.HeapBytes
	s.HeapGoalBytes += o.HeapGoalBytes
	s.Threads += o.Threads
	s.Traced += o.Traced
	s.ProcCapacity += o.ProcCapacity
	s.Running += o.Running
	s.Runnable += o.Runnable
	s.GC += o.GC
	s.MarkAssist += o.MarkAssist
	s.LockWait += o.LockWait
	s.SchedLatency.Merge(o.SchedLatency)
	s.STW.Merge(o.STW)
}

// AggregateMetric はインスタンスの 10 秒 [at, at+10s) を、その中に始まったチャンク・ログ・スナップショットから作る
func AggregateMetric(instance observation.InstanceName, at time.Time, chunks []observation.TraceChunk, logs []observation.LogLine, snapshots []observation.GoroutineSnapshot) MetricSample {
	out := MetricSample{At: at, Instance: instance}
	for _, c := range chunks {
		cs := ChunkSample(c)
		threads, heap, goal := max(out.Threads, cs.Threads), max(out.HeapBytes, cs.HeapBytes), max(out.HeapGoalBytes, cs.HeapGoalBytes)
		out.Merge(cs)
		out.Threads, out.HeapBytes, out.HeapGoalBytes = threads, heap, goal
	}
	for _, l := range logs {
		out.Logs++
		if l.Level.AtLeast(observation.LogLevelError) {
			out.Errors++
		}
	}
	if n := len(snapshots); n > 0 {
		out.Goroutines, out.HasGoroutines = len(snapshots[n-1].Goroutines), true
	}
	return out
}

// ServiceSample はインスタンスごとの同じ区切りの値を、サービスの値にまとめる
func ServiceSample(at time.Time, samples []MetricSample) MetricSample {
	out := MetricSample{At: at}
	for _, s := range samples {
		out.Merge(s)
	}
	out.Instance = ""
	return out
}

// Rollup はサービスの値を step ごとに束ねる。goroutine の数・ヒープ・スレッドはその区切りの中の最大にする
func Rollup(samples []MetricSample, from, to time.Time, step time.Duration) []MetricSample {
	byAt := map[time.Time][]MetricSample{}
	for _, s := range samples {
		byAt[s.At] = append(byAt[s.At], s)
	}
	var out []MetricSample
	for t := from.UTC().Truncate(step); t.Before(to); t = t.Add(step) {
		p := MetricSample{At: t}
		for at := t; at.Before(t.Add(step)); at = at.Add(MetricStep) {
			ss, ok := byAt[at]
			if !ok {
				continue
			}
			v := ServiceSample(at, ss)
			g, has, heap, goal, threads := max(p.Goroutines, v.Goroutines), p.HasGoroutines || v.HasGoroutines, max(p.HeapBytes, v.HeapBytes), max(p.HeapGoalBytes, v.HeapGoalBytes), max(p.Threads, v.Threads)
			p.Merge(v)
			p.Goroutines, p.HasGoroutines, p.HeapBytes, p.HeapGoalBytes, p.Threads = g, has, heap, goal, threads
		}
		out = append(out, p)
	}
	return out
}

func ratio(a, b time.Duration) float64 {
	if b <= 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func (s MetricSample) SchedP99() (time.Duration, bool) { return s.SchedLatency.Quantile(0.99) }
func (s MetricSample) GCShare() float64                { return ratio(s.GC, s.ProcCapacity) }
func (s MetricSample) MarkAssistShare() float64        { return ratio(s.MarkAssist, s.ProcCapacity) }
func (s MetricSample) LockWaitRatio() float64          { return ratio(s.LockWait, s.Running) }
func (s MetricSample) RunnableShare() float64          { return ratio(s.Runnable, s.ProcCapacity) }

// MetricStepFor は span を maxPoints 個以下で描けるいちばん細かい区切り
func MetricStepFor(span time.Duration, maxPoints int) time.Duration {
	for _, s := range []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour} {
		if span/s <= time.Duration(maxPoints) {
			return s
		}
	}
	return 24 * time.Hour
}

// markAssistOnProcs は mark assist の区間のうち、その G が P の上で走っていた時間の合計。
// mark assist の区間は G が P を降ろされている間も続くので、区間の長さをそのまま足すと P の時間を超えてしまう
func markAssistOnProcs(c observation.TraceChunk) time.Duration {
	spans := map[observation.GoroutineID][]observation.ProcSpan{}
	for _, l := range c.Procs {
		for _, sp := range l.Spans {
			spans[sp.Goroutine] = append(spans[sp.Goroutine], sp)
		}
	}
	var total time.Duration
	for _, r := range c.GC {
		if r.Kind != observation.GCMarkAssist {
			continue
		}
		for _, sp := range spans[r.Goroutine] {
			if o := min(r.End, sp.End) - max(r.Start, sp.Start); o > 0 {
				total += o
			}
		}
	}
	return total
}
