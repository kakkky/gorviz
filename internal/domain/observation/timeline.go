package observation

import (
	"cmp"
	"slices"
	"time"
)

// Slice は [from, to] を切り出し、時刻 0 を from に合わせる。from 時点の状態は先頭に時刻 0 の変化として積むので、切り出した分だけで再生できる
func (c TraceChunk) Slice(from, to time.Duration) TraceChunk {
	from, to = max(from, 0), min(to, c.Duration)
	if to < from {
		to = from
	}
	out := TraceChunk{StartedAt: c.StartedAt.Add(from), Duration: to - from, Entries: c.Entries, Threads: c.Threads}
	for _, r := range c.GC {
		if r.End < from || r.Start > to {
			continue
		}
		r.Start, r.End = max(r.Start, from)-from, min(r.End, to)-from
		out.GC = append(out.GC, r)
	}
	for _, h := range c.Heap {
		if h.At >= from && h.At <= to {
			h.At -= from
			out.Heap = append(out.Heap, h)
		}
	}
	for _, e := range c.GCEnds {
		if e.At >= from && e.At <= to {
			e.At -= from
			out.GCEnds = append(out.GCEnds, e)
		}
	}
	for _, m := range c.LogMarks {
		if m.At >= from && m.At <= to {
			m.At -= from
			out.LogMarks = append(out.LogMarks, m)
		}
	}
	lastG := map[GoroutineID]int{}
	lastP := map[ProcID]int{}
	rest := len(c.Events)
	for i, ev := range c.Events {
		if ev.At > from {
			rest = i
			break
		}
		switch ev.Kind {
		case StateChangeGoroutine:
			lastG[ev.Goroutine] = i
		case StateChangeProc:
			lastP[ev.Proc] = i
		}
	}
	initial := make([]int, 0, len(lastG)+len(lastP))
	for _, i := range lastP {
		initial = append(initial, i)
	}
	for _, i := range lastG {
		if c.Events[i].Place.Kind != PlaceDead {
			initial = append(initial, i)
		}
	}
	slices.Sort(initial)
	for _, i := range initial {
		ev := c.Events[i]
		ev.At = 0
		out.Events = append(out.Events, ev)
	}
	for _, ev := range c.Events[rest:] {
		if ev.At > to {
			break
		}
		ev.At -= from
		out.Events = append(out.Events, ev)
		if ev.Kind == StateChangeGoroutine && ev.Place.Kind == PlaceDead {
			out.GoroutinesDestroyed++
		}
		if ev.Created {
			out.GoroutinesCreated++
		}
	}
	for _, l := range c.Procs {
		lane := ProcLane{Proc: l.Proc}
		for _, s := range l.Spans {
			if s.End < from || s.Start > to {
				continue
			}
			lane.Spans = append(lane.Spans, ProcSpan{Goroutine: s.Goroutine, Thread: s.Thread, Start: max(s.Start, from) - from, End: min(s.End, to) - from})
		}
		out.Procs = append(out.Procs, lane)
	}
	return out
}

// Join は時刻の順に並んだチャンクから [from, to] に当たる部分をつなぎ、1 つのチャンクにする。集計 (Stats) はつながない。
// M の番号はチャンクごとに振り直しているので、境目をまたいで同じ番号が同じ OS のスレッドとは限らない
func Join(chunks []TraceChunk, from, to time.Time) (TraceChunk, bool) {
	var (
		out   TraceChunk
		ok    bool
		lanes = map[ProcID]int{}
	)
	for _, c := range chunks {
		lo, hi := max(c.Offset(from), 0), min(c.Offset(to), c.Duration)
		if hi <= lo {
			continue
		}
		part := c.Slice(lo, hi)
		if !ok {
			out, ok = TraceChunk{StartedAt: part.StartedAt, Entries: map[GoroutineID]string{}}, true
		}
		shift := part.StartedAt.Sub(out.StartedAt)
		for _, ev := range part.Events {
			ev.At += shift
			out.Events = append(out.Events, ev)
		}
		for _, r := range part.GC {
			r.Start, r.End = r.Start+shift, r.End+shift
			out.GC = append(out.GC, r)
		}
		for _, h := range part.Heap {
			h.At += shift
			out.Heap = append(out.Heap, h)
		}
		for _, e := range part.GCEnds {
			e.At += shift
			out.GCEnds = append(out.GCEnds, e)
		}
		for _, m := range part.LogMarks {
			m.At += shift
			out.LogMarks = append(out.LogMarks, m)
		}
		for _, l := range part.Procs {
			i, seen := lanes[l.Proc]
			if !seen {
				i = len(out.Procs)
				lanes[l.Proc] = i
				out.Procs = append(out.Procs, ProcLane{Proc: l.Proc})
			}
			for _, s := range l.Spans {
				s.Start, s.End = s.Start+shift, s.End+shift
				out.Procs[i].Spans = append(out.Procs[i].Spans, s)
			}
		}
		for g, e := range part.Entries {
			if _, has := out.Entries[g]; !has {
				out.Entries[g] = e
			}
		}
		out.GoroutinesCreated += part.GoroutinesCreated
		out.GoroutinesDestroyed += part.GoroutinesDestroyed
		out.GCCycles += part.GCCycles
		out.Threads = max(out.Threads, part.Threads)
		out.Duration = shift + part.Duration
	}
	slices.SortFunc(out.Procs, func(a, b ProcLane) int { return cmp.Compare(a.Proc, b.Proc) })
	return out, ok
}
