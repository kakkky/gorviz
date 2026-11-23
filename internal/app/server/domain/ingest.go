package domain

import (
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// IngestPlan は 1 つの Batch のうち書くもの
type IngestPlan struct {
	Logs        []observation.LogLine
	TraceChunks []observation.TraceChunk
	Snapshots   []observation.GoroutineSnapshot
	LeakChecks  []observation.GoroutineLeakCheck
}

func (p IngestPlan) Empty() bool {
	return len(p.Logs) == 0 && len(p.TraceChunks) == 0 && len(p.Snapshots) == 0 && len(p.LeakChecks) == 0
}

// PlanIngest は server が持っている位置 held と突き合わせ、書くものを決める。
// agent の思っている位置 (After) が held より先の種類は、抜けがあるので書かない。held までのもの (送り直し) と、保持期間を過ぎたものは捨てる
func PlanIngest(b observation.Batch, held observation.Cursor, r Retention, now time.Time) IngestPlan {
	var p IngestPlan
	if b.After.LogAgent == "" || b.After.LogSeq <= held.LogSeqFor(b.After.LogAgent) {
		for _, l := range b.Logs {
			if l.Seq > held.LogSeqFor(l.Agent) && r.Keeps(DataLogs, l.At, now) {
				p.Logs = append(p.Logs, l)
			}
		}
	}
	if !b.After.TraceChunk.After(held.TraceChunk) {
		for _, c := range b.TraceChunks {
			if c.StartedAt.After(held.TraceChunk) && r.Keeps(DataExecutionTraces, c.StartedAt, now) {
				p.TraceChunks = append(p.TraceChunks, c)
			}
		}
	}
	if !b.After.Snapshot.After(held.Snapshot) {
		for _, s := range b.Snapshots {
			if s.At.After(held.Snapshot) && r.Keeps(DataGoroutineSnapshots, s.At, now) {
				p.Snapshots = append(p.Snapshots, s)
			}
		}
	}
	if !b.After.LeakCheck.After(held.LeakCheck) {
		for _, l := range b.LeakChecks {
			if l.At.After(held.LeakCheck) && r.Keeps(DataGoroutineLeakChecks, l.At, now) {
				p.LeakChecks = append(p.LeakChecks, l)
			}
		}
	}
	return p
}

// Advance は held に書いたものを足した位置。どれも列の順に並んでいるので、最後のものが新しい位置になる
func (p IngestPlan) Advance(held observation.Cursor) observation.Cursor {
	out := held
	for _, l := range p.Logs {
		if l.Agent != out.LogAgent || l.Seq > out.LogSeq {
			out.LogAgent, out.LogSeq = l.Agent, l.Seq
		}
	}
	for _, c := range p.TraceChunks {
		if c.StartedAt.After(out.TraceChunk) {
			out.TraceChunk = c.StartedAt
		}
	}
	for _, s := range p.Snapshots {
		if s.At.After(out.Snapshot) {
			out.Snapshot = s.At
		}
	}
	for _, l := range p.LeakChecks {
		if l.At.After(out.LeakCheck) {
			out.LeakCheck = l.At
		}
	}
	return out
}

// Latest は Batch の中で一番新しい観測の時刻
func Latest(b observation.Batch) (time.Time, bool) {
	var t time.Time
	for _, l := range b.Logs {
		t = maxTime(t, l.At)
	}
	for _, c := range b.TraceChunks {
		t = maxTime(t, c.EndedAt())
	}
	for _, s := range b.Snapshots {
		t = maxTime(t, s.At)
	}
	for _, l := range b.LeakChecks {
		t = maxTime(t, l.At)
	}
	return t, !t.IsZero()
}

// Earliest は Batch の中で一番古い観測の時刻
func Earliest(b observation.Batch) (time.Time, bool) {
	var t time.Time
	set := func(v time.Time) {
		if t.IsZero() || v.Before(t) {
			t = v
		}
	}
	for _, l := range b.Logs {
		set(l.At)
	}
	for _, c := range b.TraceChunks {
		set(c.StartedAt)
	}
	for _, s := range b.Snapshots {
		set(s.At)
	}
	for _, l := range b.LeakChecks {
		set(l.At)
	}
	return t, !t.IsZero()
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// Holding はインスタンスの観測データをディスクにどこまで持っているか。TraceEnd は最後のチャンクの終わり、Latest はどの種類も含めて一番新しい時刻
type Holding struct {
	Cursor   observation.Cursor
	TraceEnd time.Time
	Latest   time.Time
}

func (p IngestPlan) AdvanceHolding(h Holding) Holding {
	out := Holding{Cursor: p.Advance(h.Cursor), TraceEnd: h.TraceEnd, Latest: h.Latest}
	for _, c := range p.TraceChunks {
		out.TraceEnd = maxTime(out.TraceEnd, c.EndedAt())
	}
	if t, ok := Latest(observation.Batch{Logs: p.Logs, TraceChunks: p.TraceChunks, Snapshots: p.Snapshots, LeakChecks: p.LeakChecks}); ok {
		out.Latest = maxTime(out.Latest, t)
	}
	return out
}

func (h Holding) Arrival(ended bool) Arrival {
	return Arrival{TraceEnd: h.TraceEnd, Latest: h.Latest, Ended: ended}
}
