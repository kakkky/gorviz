package domain

import (
	"fmt"
	"time"
)

// 閾値は固定で、利用者には変えさせない。根拠は docs/design.md の「判定」
const (
	cpuContentionP99     = time.Millisecond
	gcCPUShareFact       = 0.5
	markAssistShare      = 0.05
	longSTW              = time.Millisecond
	threadLimit          = 10000
	threadExplosionCount = threadLimit / 2
	minRunnableSamples   = 20
	minLockWait          = 10 * time.Millisecond
)

// WindowStats は期間にかかる時間幅を足し合わせた値
type WindowStats struct {
	From         time.Duration
	To           time.Duration
	Procs        int
	RunnableWait Histogram
	Running      time.Duration
	LockWait     time.Duration
	GCWorker     time.Duration
	MarkAssist   time.Duration
	STW          time.Duration
	MaxThreads   int
	STWs         []TimeRange
}

func NewWindowStats(ix *TraceIndex, from, to time.Duration) WindowStats {
	w := WindowStats{From: from, To: to, Procs: ix.Procs()}
	for _, b := range ix.Buckets(from, to) {
		w.RunnableWait.Merge(&b.RunnableWait)
		w.Running += b.Running
		w.LockWait += b.LockWait
		w.GCWorker += b.GCWorker
		w.MarkAssist += b.MarkAssist
		w.STW += b.STW
		w.MaxThreads = max(w.MaxThreads, b.Threads)
	}
	w.STWs = ix.AppSTW(from, to)
	return w
}

func (w WindowStats) capacity() time.Duration {
	return time.Duration(w.Procs) * max(w.To-w.From, time.Millisecond)
}

// GCCPUShare は GC の worker・mark assist・STW で止めた分が、P の時間に占める割合
func (w WindowStats) GCCPUShare() float64 {
	return float64(w.GCWorker+w.MarkAssist+w.STW*time.Duration(w.Procs)) / float64(w.capacity())
}

func (w WindowStats) MarkAssistShare() float64 {
	return float64(w.MarkAssist) / float64(w.capacity())
}

// DetectRuntimeIssues は期間の値から GOROUTINE_LEAK 以外の Issue を判定する
func DetectRuntimeIssues(w WindowStats) []Detection {
	var out []Detection
	d := func(kind IssueKind, target string, c Certainty, value string) {
		out = append(out, Detection{Kind: kind, Target: target, Certainty: c, From: w.From, To: w.To, Value: value})
	}
	if w.RunnableWait.Total >= minRunnableSamples {
		if p99 := w.RunnableWait.Quantile(0.99); p99 >= cpuContentionP99 {
			d(IssueCPUContention, "", CertaintyInference, fmt.Sprintf("p99 of the wait from RUNNABLE to RUNNING was %s", formatDuration(p99)))
		}
	}
	if share := w.GCCPUShare(); share >= gcCPUShareFact {
		d(IssueGCPressure, "", CertaintyFact, fmt.Sprintf("GC used %.0f%% of the CPU", share*100))
	} else if assist := w.MarkAssistShare(); assist > markAssistShare {
		d(IssueGCPressure, "", CertaintyInference, fmt.Sprintf("mark assist used %.1f%% of the CPU", assist*100))
	}
	if w.LockWait >= minLockWait && w.LockWait > w.Running {
		d(IssueLockContention, "", CertaintyInference, fmt.Sprintf("goroutines waited %s on locks while running %s", formatDuration(w.LockWait), formatDuration(w.Running)))
	}
	longest := map[string]time.Duration{}
	for _, s := range w.STWs {
		if dur := s.End - s.Start; dur >= longSTW && dur > longest[s.Reason] {
			longest[s.Reason] = dur
		}
	}
	for reason, dur := range longest {
		d(IssueLongSTW, reason, CertaintyInference, fmt.Sprintf("a stop-the-world (%s) took %s", reason, formatDuration(dur)))
	}
	if w.MaxThreads > threadExplosionCount {
		d(IssueThreadExplosion, "", CertaintyInference, fmt.Sprintf("%d OS threads, over half of the limit %d", w.MaxThreads, threadLimit))
	}
	return out
}

func formatDuration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
	case d >= time.Microsecond:
		return fmt.Sprintf("%.1fµs", float64(d)/float64(time.Microsecond))
	}
	return fmt.Sprintf("%dns", d)
}

// FormatDuration は時間を読みやすい桁で書く
func FormatDuration(d time.Duration) string { return formatDuration(d) }
