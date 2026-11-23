package agentrpc

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/pkg/histogram"
)

func ToExportRequest(batches []observation.Batch) *ExportRequest {
	req := &ExportRequest{}
	for _, b := range batches {
		req.Batches = append(req.Batches, toBatch(b))
	}
	return req
}

func FromExportRequest(req *ExportRequest) []observation.Batch {
	out := make([]observation.Batch, 0, len(req.GetBatches()))
	for _, b := range req.GetBatches() {
		out = append(out, fromBatch(b))
	}
	return out
}

func ToExportResponse(cursors map[observation.Instance]observation.Cursor) *ExportResponse {
	res := &ExportResponse{}
	for in, c := range cursors {
		res.Cursors = append(res.Cursors, &InstanceCursor{Instance: toInstance(in), Cursor: toCursor(c)})
	}
	return res
}

func FromExportResponse(res *ExportResponse) map[observation.Instance]observation.Cursor {
	out := make(map[observation.Instance]observation.Cursor, len(res.GetCursors()))
	for _, c := range res.GetCursors() {
		out[fromInstance(c.GetInstance())] = fromCursor(c.GetCursor())
	}
	return out
}

func toInstance(in observation.Instance) *Instance {
	return &Instance{Service: string(in.Service), Name: string(in.Name), Incarnation: string(in.Incarnation)}
}

func fromInstance(in *Instance) observation.Instance {
	return observation.Instance{Service: observation.ServiceName(in.GetService()), Name: observation.InstanceName(in.GetName()), Incarnation: observation.Incarnation(in.GetIncarnation())}
}

func toTime(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func fromTime(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.AsTime()
}

func toCursor(c observation.Cursor) *Cursor {
	return &Cursor{LogAgent: string(c.LogAgent), LogSeq: c.LogSeq, TraceChunk: toTime(c.TraceChunk), Snapshot: toTime(c.Snapshot), LeakCheck: toTime(c.LeakCheck)}
}

func fromCursor(c *Cursor) observation.Cursor {
	return observation.Cursor{LogAgent: observation.AgentID(c.GetLogAgent()), LogSeq: c.GetLogSeq(), TraceChunk: fromTime(c.GetTraceChunk()), Snapshot: fromTime(c.GetSnapshot()), LeakCheck: fromTime(c.GetLeakCheck())}
}

func toBatch(b observation.Batch) *Batch {
	out := &Batch{Instance: toInstance(b.Instance), After: toCursor(b.After), Ended: b.Ended}
	for _, l := range b.Logs {
		out.Logs = append(out.Logs, ToLogLine(l))
	}
	for _, c := range b.TraceChunks {
		out.TraceChunks = append(out.TraceChunks, ToTraceChunk(c))
	}
	for _, s := range b.Snapshots {
		out.Snapshots = append(out.Snapshots, ToGoroutineSnapshot(s))
	}
	for _, l := range b.LeakChecks {
		out.LeakChecks = append(out.LeakChecks, ToGoroutineLeakCheck(l))
	}
	return out
}

func fromBatch(b *Batch) observation.Batch {
	out := observation.Batch{Instance: fromInstance(b.GetInstance()), After: fromCursor(b.GetAfter()), Ended: b.GetEnded()}
	for _, l := range b.GetLogs() {
		out.Logs = append(out.Logs, FromLogLine(l))
	}
	for _, c := range b.GetTraceChunks() {
		out.TraceChunks = append(out.TraceChunks, FromTraceChunk(c))
	}
	for _, s := range b.GetSnapshots() {
		out.Snapshots = append(out.Snapshots, FromGoroutineSnapshot(s))
	}
	for _, l := range b.GetLeakChecks() {
		out.LeakChecks = append(out.LeakChecks, FromGoroutineLeakCheck(l))
	}
	return out
}

func ToLogLine(l observation.LogLine) *LogLine {
	out := &LogLine{Agent: string(l.Agent), Seq: l.Seq, At: toTime(l.At), TimeFromLog: l.TimeFromLog, Level: string(l.Level), Message: l.Message, Raw: l.Raw, Source: string(l.Source)}
	if l.HasTraceSeq {
		out.TraceSeq = &l.TraceSeq
	}
	if l.HasDuration {
		d := int64(l.Duration)
		out.DurationNs = &d
	}
	return out
}

func FromLogLine(l *LogLine) observation.LogLine {
	return observation.LogLine{
		Agent: observation.AgentID(l.GetAgent()), Seq: l.GetSeq(), At: fromTime(l.GetAt()), TimeFromLog: l.GetTimeFromLog(),
		Level: observation.LogLevel(l.GetLevel()), Message: l.GetMessage(), Raw: l.GetRaw(), Source: observation.LogSource(l.GetSource()),
		TraceSeq: l.GetTraceSeq(), HasTraceSeq: l.TraceSeq != nil, Duration: time.Duration(l.GetDurationNs()), HasDuration: l.DurationNs != nil,
	}
}

func toFrame(f observation.Frame) *Frame {
	if f == (observation.Frame{}) {
		return nil
	}
	return &Frame{Function: f.Function, File: f.File, Line: int64(f.Line)}
}

func fromFrame(f *Frame) observation.Frame {
	return observation.Frame{Function: f.GetFunction(), File: f.GetFile(), Line: int(f.GetLine())}
}

func toFrames(fs []observation.Frame) []*Frame {
	out := make([]*Frame, 0, len(fs))
	for _, f := range fs {
		out = append(out, &Frame{Function: f.Function, File: f.File, Line: int64(f.Line)})
	}
	return out
}

func fromFrames(fs []*Frame) []observation.Frame {
	if len(fs) == 0 {
		return nil
	}
	out := make([]observation.Frame, 0, len(fs))
	for _, f := range fs {
		out = append(out, fromFrame(f))
	}
	return out
}

func ToGoroutineSnapshot(s observation.GoroutineSnapshot) *GoroutineSnapshot {
	out := &GoroutineSnapshot{At: toTime(s.At)}
	for _, g := range s.Goroutines {
		out.Goroutines = append(out.Goroutines, &Goroutine{
			Id: int64(g.ID), State: string(g.State), WaitReason: g.WaitReason, WaitReasonInferred: g.WaitReasonInferred,
			WaitingNs: int64(g.Waiting), WaitingLowerBound: g.WaitingLowerBound, Entry: g.Entry, Current: toFrame(g.Current),
			CreatedBy: toFrame(g.CreatedBy), CreatedByInferred: g.CreatedByInferred, Stack: toFrames(g.Stack),
			WokenBy: int64(g.WokenBy), WokenByAt: toFrame(g.WokenByAt),
		})
	}
	return out
}

func FromGoroutineSnapshot(s *GoroutineSnapshot) observation.GoroutineSnapshot {
	out := observation.GoroutineSnapshot{At: fromTime(s.GetAt())}
	for _, g := range s.GetGoroutines() {
		out.Goroutines = append(out.Goroutines, observation.Goroutine{
			ID: observation.GoroutineID(g.GetId()), State: observation.GoroutineState(g.GetState()), WaitReason: g.GetWaitReason(),
			WaitReasonInferred: g.GetWaitReasonInferred(), Waiting: time.Duration(g.GetWaitingNs()), WaitingLowerBound: g.GetWaitingLowerBound(),
			Entry: g.GetEntry(), Current: fromFrame(g.GetCurrent()), CreatedBy: fromFrame(g.GetCreatedBy()), CreatedByInferred: g.GetCreatedByInferred(),
			Stack: fromFrames(g.GetStack()), WokenBy: observation.GoroutineID(g.GetWokenBy()), WokenByAt: fromFrame(g.GetWokenByAt()),
		})
	}
	return out
}

func ToGoroutineLeakCheck(l observation.GoroutineLeakCheck) *GoroutineLeakCheck {
	out := &GoroutineLeakCheck{At: toTime(l.At), Status: string(l.Status)}
	for _, s := range l.Leaks {
		out.Leaks = append(out.Leaks, &LeakedStack{Stack: toFrames(s.Stack), Count: int64(s.Count)})
	}
	return out
}

func FromGoroutineLeakCheck(l *GoroutineLeakCheck) observation.GoroutineLeakCheck {
	out := observation.GoroutineLeakCheck{At: fromTime(l.GetAt()), Status: observation.LeakCheckStatus(l.GetStatus())}
	for _, s := range l.GetLeaks() {
		out.Leaks = append(out.Leaks, observation.LeakedStack{Stack: fromFrames(s.GetStack()), Count: int(s.GetCount())})
	}
	return out
}

func toHistogram(h histogram.Histogram) *Histogram {
	return &Histogram{Counts: h.Counts, MaxNs: int64(h.Max)}
}

func fromHistogram(h *Histogram) histogram.Histogram {
	return histogram.Histogram{Counts: h.GetCounts(), Max: time.Duration(h.GetMaxNs())}
}

func ToTraceChunk(c observation.TraceChunk) *TraceChunk {
	out := &TraceChunk{
		StartedAt: toTime(c.StartedAt), DurationNs: int64(c.Duration), Entries: make(map[int64]string, len(c.Entries)),
		GoroutinesCreated: int64(c.GoroutinesCreated), GoroutinesDestroyed: int64(c.GoroutinesDestroyed), GcCycles: int64(c.GCCycles), Threads: int64(c.Threads),
	}
	for _, e := range c.Events {
		out.Events = append(out.Events, &StateChange{
			AtNs: int64(e.At), Kind: string(e.Kind), Goroutine: int64(e.Goroutine),
			Place:    &Place{Kind: string(e.Place.Kind), Proc: int64(e.Place.Proc), HasProc: e.Place.HasProc, Thread: int64(e.Place.Thread), Reason: e.Place.Reason, Cgo: e.Place.Cgo},
			Function: e.Function, Location: e.Location, Created: e.Created, WokenBy: int64(e.WokenBy), WokenByFunction: e.WokenByFunction, WokenByLocation: e.WokenByLocation,
			Proc: int64(e.Proc), ProcStatus: string(e.ProcStatus), Thread: int64(e.Thread),
		})
	}
	for _, l := range c.Procs {
		lane := &ProcLane{Proc: int64(l.Proc)}
		for _, s := range l.Spans {
			lane.Spans = append(lane.Spans, &ProcSpan{Goroutine: int64(s.Goroutine), Thread: int64(s.Thread), StartNs: int64(s.Start), EndNs: int64(s.End)})
		}
		out.Procs = append(out.Procs, lane)
	}
	for g, e := range c.Entries {
		out.Entries[int64(g)] = e
	}
	for _, r := range c.GC {
		out.Gc = append(out.Gc, &GCRange{Kind: string(r.Kind), Goroutine: int64(r.Goroutine), Reason: r.Reason, StartNs: int64(r.Start), EndNs: int64(r.End)})
	}
	for _, h := range c.Heap {
		out.Heap = append(out.Heap, &HeapSample{AtNs: int64(h.At), ObjectsBytes: h.ObjectsBytes, GoalBytes: h.GoalBytes})
	}
	for _, e := range c.GCEnds {
		out.GcEnds = append(out.GcEnds, &GCEnd{AtNs: int64(e.At), LiveHeapBytes: e.LiveHeapBytes, HeapGoalBytes: e.HeapGoalBytes})
	}
	for _, m := range c.LogMarks {
		out.LogMarks = append(out.LogMarks, &LogMark{Seq: m.Seq, Goroutine: int64(m.Goroutine), AtNs: int64(m.At)})
	}
	s := c.Stats
	out.Stats = &TraceStats{
		Procs: int64(s.Procs), RunnableNs: int64(s.Runnable), SchedLatency: toHistogram(s.SchedLatency), Stw: toHistogram(s.STW),
		GcMaxShare: s.GCMaxShare, ProcRunningNs: int64(s.ProcRunning), ProcGcNs: int64(s.ProcGC),
	}
	for _, w := range s.Waits {
		out.Stats.Waits = append(out.Stats.Waits, &WaitSite{Kind: string(w.Kind), Reason: w.Reason, Cgo: w.Cgo, Entry: w.Entry, Function: w.Function, Location: w.Location, Count: int64(w.Count), TotalNs: int64(w.Total), MaxNs: int64(w.Max)})
	}
	return out
}

func FromTraceChunk(c *TraceChunk) observation.TraceChunk {
	out := observation.TraceChunk{
		StartedAt: fromTime(c.GetStartedAt()), Duration: time.Duration(c.GetDurationNs()), Entries: make(map[observation.GoroutineID]string, len(c.GetEntries())),
		GoroutinesCreated: int(c.GetGoroutinesCreated()), GoroutinesDestroyed: int(c.GetGoroutinesDestroyed()), GCCycles: int(c.GetGcCycles()), Threads: int(c.GetThreads()),
	}
	for _, e := range c.GetEvents() {
		p := e.GetPlace()
		out.Events = append(out.Events, observation.StateChange{
			At: time.Duration(e.GetAtNs()), Kind: observation.StateChangeKind(e.GetKind()), Goroutine: observation.GoroutineID(e.GetGoroutine()),
			Place:    observation.Place{Kind: observation.PlaceKind(p.GetKind()), Proc: observation.ProcID(p.GetProc()), HasProc: p.GetHasProc(), Thread: observation.ThreadID(p.GetThread()), Reason: p.GetReason(), Cgo: p.GetCgo()},
			Function: e.GetFunction(), Location: e.GetLocation(), Created: e.GetCreated(), WokenBy: observation.GoroutineID(e.GetWokenBy()),
			WokenByFunction: e.GetWokenByFunction(), WokenByLocation: e.GetWokenByLocation(),
			Proc: observation.ProcID(e.GetProc()), ProcStatus: observation.ProcStatus(e.GetProcStatus()), Thread: observation.ThreadID(e.GetThread()),
		})
	}
	for _, l := range c.GetProcs() {
		lane := observation.ProcLane{Proc: observation.ProcID(l.GetProc())}
		for _, s := range l.GetSpans() {
			lane.Spans = append(lane.Spans, observation.ProcSpan{Goroutine: observation.GoroutineID(s.GetGoroutine()), Thread: observation.ThreadID(s.GetThread()), Start: time.Duration(s.GetStartNs()), End: time.Duration(s.GetEndNs())})
		}
		out.Procs = append(out.Procs, lane)
	}
	for g, e := range c.GetEntries() {
		out.Entries[observation.GoroutineID(g)] = e
	}
	for _, r := range c.GetGc() {
		out.GC = append(out.GC, observation.GCRange{Kind: observation.GCRangeKind(r.GetKind()), Goroutine: observation.GoroutineID(r.GetGoroutine()), Reason: r.GetReason(), Start: time.Duration(r.GetStartNs()), End: time.Duration(r.GetEndNs())})
	}
	for _, h := range c.GetHeap() {
		out.Heap = append(out.Heap, observation.HeapSample{At: time.Duration(h.GetAtNs()), ObjectsBytes: h.GetObjectsBytes(), GoalBytes: h.GetGoalBytes()})
	}
	for _, e := range c.GetGcEnds() {
		out.GCEnds = append(out.GCEnds, observation.GCEnd{At: time.Duration(e.GetAtNs()), LiveHeapBytes: e.GetLiveHeapBytes(), HeapGoalBytes: e.GetHeapGoalBytes()})
	}
	for _, m := range c.GetLogMarks() {
		out.LogMarks = append(out.LogMarks, observation.LogMark{Seq: m.GetSeq(), Goroutine: observation.GoroutineID(m.GetGoroutine()), At: time.Duration(m.GetAtNs())})
	}
	s := c.GetStats()
	out.Stats = observation.TraceStats{
		Procs: int(s.GetProcs()), Runnable: time.Duration(s.GetRunnableNs()), SchedLatency: fromHistogram(s.GetSchedLatency()), STW: fromHistogram(s.GetStw()),
		GCMaxShare: s.GetGcMaxShare(), ProcRunning: time.Duration(s.GetProcRunningNs()), ProcGC: time.Duration(s.GetProcGcNs()),
	}
	for _, w := range s.GetWaits() {
		out.Stats.Waits = append(out.Stats.Waits, observation.WaitSite{
			Kind: observation.PlaceKind(w.GetKind()), Reason: w.GetReason(), Cgo: w.GetCgo(), Entry: w.GetEntry(), Function: w.GetFunction(), Location: w.GetLocation(),
			Count: int(w.GetCount()), Total: time.Duration(w.GetTotalNs()), Max: time.Duration(w.GetMaxNs()),
		})
	}
	return out
}
