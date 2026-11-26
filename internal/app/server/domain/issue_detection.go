package domain

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
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

func NewWindowStats(ix *ObservationSet, from, to time.Duration) WindowStats {
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

// LeakProfileStack は goroutineleak プロファイルの 1 つのスタック。Count はそのスタックでリークした goroutine の数
type LeakProfileStack struct {
	Count  int
	Frames []Frame
}

// ParseGoroutineLeakProfile は debug=1 の goroutineleak プロファイルを読む。
// "N @ 0x..." の行がスタックの始まりで、続く "#\t0x...\tfunc+0x..\tfile:line" の行がフレーム
func ParseGoroutineLeakProfile(text []byte) ([]LeakProfileStack, error) {
	var out []LeakProfileStack
	sc := bufio.NewScanner(bytes.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "goroutineleak profile:"):
		case strings.Contains(line, " @ "):
			n, err := strconv.Atoi(strings.TrimSpace(line[:strings.Index(line, " @ ")]))
			if err != nil {
				return nil, ErrInvalidArgument.Wrap(err, "read goroutineleak profile count")
			}
			out = append(out, LeakProfileStack{Count: n})
		case strings.HasPrefix(line, "#") && len(out) > 0:
			fields := strings.Split(strings.TrimPrefix(line, "#"), "\t")
			var parts []string
			for _, f := range fields {
				if f = strings.TrimSpace(f); f != "" {
					parts = append(parts, f)
				}
			}
			if len(parts) < 3 {
				continue
			}
			fn := parts[1]
			if i := strings.LastIndex(fn, "+0x"); i > 0 {
				fn = fn[:i]
			}
			file, lineNo := parts[2], 0
			if i := strings.LastIndex(file, ":"); i > 0 {
				lineNo, _ = strconv.Atoi(file[i+1:])
				file = file[:i]
			}
			last := &out[len(out)-1]
			last.Frames = append(last.Frames, Frame{Function: fn, File: file, Line: lineNo})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, ErrInvalidArgument.Wrap(err, "read goroutineleak profile")
	}
	return out, nil
}

type leakCandidate struct {
	g     *GoroutineRecord
	since time.Duration
	why   string
}

func placeKey(f Frame) string {
	return f.Function + "@" + f.Location()
}

// MatchLeaks はプロファイルのスタックごとの件数を、同じ時刻に同じ場所で待っていた goroutine と突き合わせ、生成元ごとの Detection にする。
// 同じ場所で待つ goroutine がちょうど件数だけあれば全部をリークとし (FACT)、多ければ待っている時間が長い順に件数分を選ぶ (INFERENCE)
func MatchLeaks(ix *ObservationSet, at time.Duration, stacks []LeakProfileStack) []Detection {
	waiting := map[string][]leakCandidate{}
	for _, id := range ix.GoroutineIDs() {
		g, _ := ix.Goroutine(id)
		if !g.App() || (g.Ended && g.EndedAt <= at) {
			continue
		}
		c, ok := ix.StateAt(g, at)
		if !ok || c.Place != PlaceWaiting {
			continue
		}
		f := CurrentFrame(ix.Stack(c.Stack))
		waiting[placeKey(f)] = append(waiting[placeKey(f)], leakCandidate{g: g, since: c.At, why: ix.Reason(c.Reason)})
	}
	byCreator := map[string]*Detection{}
	var order []string
	for _, s := range stacks {
		if s.Count <= 0 || len(s.Frames) == 0 {
			continue
		}
		blocked := CurrentFrame(s.Frames)
		if IsAgentFunction(blocked.Function) || IsAgentFunction(EntryFunction(s.Frames)) {
			continue
		}
		cands := slices.Clone(waiting[placeKey(blocked)])
		slices.SortFunc(cands, func(a, b leakCandidate) int {
			return cmp.Or(cmp.Compare(a.since, b.since), cmp.Compare(a.g.ID, b.g.ID))
		})
		ls := LeakStack{Blocked: blocked, Count: s.Count, Of: max(len(cands), s.Count), Certainty: CertaintyFact}
		picked := cands
		if len(cands) > s.Count {
			picked = cands[:s.Count]
			ls.Certainty = CertaintyInference
		}
		for _, c := range picked {
			ls.Goroutines = append(ls.Goroutines, c.g.ID)
		}
		if len(picked) > 0 {
			ls.Creator, ls.Reason = picked[0].g.Creator, picked[0].why
		}
		if ls.Creator.Function == "" {
			ls.Creator = Frame{Function: EntryFunction(s.Frames)}
		}
		target := IssueFunction(ls.Creator.Function)
		d, ok := byCreator[target]
		if !ok {
			d = &Detection{Kind: IssueGoroutineLeak, Target: target, Certainty: CertaintyFact, From: at, To: at}
			byCreator[target] = d
			order = append(order, target)
		}
		d.Leaks = append(d.Leaks, ls)
	}
	out := make([]Detection, 0, len(order))
	for _, t := range order {
		d := byCreator[t]
		n := 0
		for _, l := range d.Leaks {
			n += l.Count
		}
		d.Value = fmt.Sprintf("%d goroutines leaked", n)
		out = append(out, *d)
	}
	return out
}

// LeakOf は、goroutine がリークの判定に入っているか
func LeakOf(leaks []LeakStack, g GoroutineID) (LeakStack, bool) {
	for _, l := range leaks {
		if slices.Contains(l.Goroutines, g) {
			return l, true
		}
	}
	return LeakStack{}, false
}
