package agentrpc

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// New〜 は model を送る形にし、Model は受け取ったものを model に戻す。nil の Model は model の空の値を返す

func NewLogLines(ls []model.LogLine) []*LogLine {
	return each(ls, func(l model.LogLine) *LogLine {
		out := &LogLine{
			Id: int64(l.ID), At: Timestamp(l.At), TimeFromLog: l.TimeFromLog, Level: logLevels.to(l.Level),
			Message: l.Message, Raw: l.Raw, Source: l.Source,
		}
		if l.HasTraceSeq {
			out.TraceSeq = &l.TraceSeq
		}
		if l.HasDuration {
			d := int64(l.Duration)
			out.DurationNs = &d
		}
		return out
	})
}

func (x *LogLine) Model() model.LogLine {
	return model.LogLine{
		ID: model.LogLineID(x.GetId()), At: TimeOf(x.GetAt()), TimeFromLog: x.GetTimeFromLog(), Level: logLevels.from(x.GetLevel()),
		Message: x.GetMessage(), Raw: x.GetRaw(), Source: x.GetSource(),
		TraceSeq: x.GetTraceSeq(), HasTraceSeq: x != nil && x.TraceSeq != nil,
		Duration: time.Duration(x.GetDurationNs()), HasDuration: x != nil && x.DurationNs != nil,
	}
}

func NewGoroutineDump(d model.GoroutineDump) *GoroutineDump {
	return &GoroutineDump{
		At: Timestamp(d.At), LeakCheck: leakChecks.to(d.LeakCheck),
		Goroutines: each(d.Goroutines, func(g model.Goroutine) *Goroutine {
			return &Goroutine{
				Id: int64(g.ID), State: g.State, WaitNs: int64(g.Wait), EntryFunction: g.EntryFunction, CurrentFunction: g.CurrentFunction,
				CreatedBy: g.CreatedBy, CreatedAt: g.CreatedAt, Leaked: g.Leaked, Labels: g.Labels,
			}
		}),
	}
}

func (x *GoroutineDump) Model() model.GoroutineDump {
	return model.GoroutineDump{
		At: TimeOf(x.GetAt()), LeakCheck: leakChecks.from(x.GetLeakCheck()),
		Goroutines: each(x.GetGoroutines(), func(g *Goroutine) model.Goroutine {
			return model.Goroutine{
				ID: model.GoroutineID(g.GetId()), State: g.GetState(), Wait: time.Duration(g.GetWaitNs()), EntryFunction: g.GetEntryFunction(),
				CurrentFunction: g.GetCurrentFunction(), CreatedBy: g.GetCreatedBy(), CreatedAt: g.GetCreatedAt(), Leaked: g.GetLeaked(), Labels: g.GetLabels(),
			}
		}),
	}
}

func NewTraceChunk(c model.TraceChunk) *TraceChunk {
	return &TraceChunk{
		StartedAt: Timestamp(c.StartedAt), Timeline: newTimeline(c.Timeline), LogMarks: each(c.LogMarks, NewTraceLogMark),
		Stats: newStats(c.Stats), Raw: c.Raw,
	}
}

func (x *TraceChunk) Model() model.TraceChunk {
	return model.TraceChunk{
		StartedAt: TimeOf(x.GetStartedAt()), Timeline: x.GetTimeline().model(),
		LogMarks: each(x.GetLogMarks(), (*TraceLogMark).Model), Stats: x.GetStats().model(), Raw: x.GetRaw(),
	}
}

func NewTraceLogMark(m model.TraceLogMark) *TraceLogMark {
	return &TraceLogMark{Seq: m.Seq, Goroutine: int64(m.Goroutine), AtNs: int64(m.At)}
}

func (x *TraceLogMark) Model() model.TraceLogMark {
	return model.TraceLogMark{Seq: x.GetSeq(), Goroutine: model.GoroutineID(x.GetGoroutine()), At: time.Duration(x.GetAtNs())}
}

func NewRecordingSpan(s model.RecordingSpan) *RecordingSpan {
	return &RecordingSpan{From: Timestamp(s.From), To: Timestamp(s.To), Bytes: s.Bytes, LimitBytes: s.LimitBytes}
}

func (x *RecordingSpan) Model() model.RecordingSpan {
	return model.RecordingSpan{From: TimeOf(x.GetFrom()), To: TimeOf(x.GetTo()), Bytes: x.GetBytes(), LimitBytes: x.GetLimitBytes()}
}

func newTimeline(t model.SchedulerTimeline) *SchedulerTimeline {
	out := &SchedulerTimeline{
		WindowNs: int64(t.Window), GoroutinesCreated: int64(t.GoroutinesCreated), GoroutinesDestroyed: int64(t.GoroutinesDestroyed),
		GcCycles: int64(t.GCCycles),
		Threads:  each(t.Threads, func(th model.ThreadID) int64 { return int64(th) }),
		Procs: each(t.Procs, func(l model.ProcLane) *ProcLane {
			return &ProcLane{Proc: int64(l.Proc), Spans: each(l.Spans, func(s model.SchedSpan) *SchedSpan {
				return &SchedSpan{Goroutine: int64(s.Goroutine), Thread: int64(s.Thread), StartNs: int64(s.Start), EndNs: int64(s.End)}
			})}
		}),
		Events: each(t.Events, newEvent),
		Gc: each(t.GC, func(r model.GCRange) *GCRange {
			return &GCRange{Kind: gcKinds.to(r.Kind), Goroutine: int64(r.Goroutine), Reason: r.Reason, StartNs: int64(r.Start), EndNs: int64(r.End)}
		}),
		Heap: each(t.Heap, func(h model.HeapSample) *HeapSample {
			return &HeapSample{AtNs: int64(h.At), ObjectsBytes: h.ObjectsBytes, GoalBytes: h.GoalBytes}
		}),
		GcEnds: each(t.GCEnds, func(e model.GCEnd) *GCEnd {
			return &GCEnd{AtNs: int64(e.At), LiveHeapBytes: e.LiveHeapBytes, HeapGoalBytes: e.HeapGoalBytes}
		}),
	}
	if len(t.Entries) > 0 {
		out.Entries = make(map[int64]string, len(t.Entries))
		for g, e := range t.Entries {
			out.Entries[int64(g)] = e
		}
	}
	return out
}

func (x *SchedulerTimeline) model() model.SchedulerTimeline {
	out := model.SchedulerTimeline{
		Window: time.Duration(x.GetWindowNs()), GoroutinesCreated: int(x.GetGoroutinesCreated()), GoroutinesDestroyed: int(x.GetGoroutinesDestroyed()),
		GCCycles: int(x.GetGcCycles()),
		Threads:  each(x.GetThreads(), func(th int64) model.ThreadID { return model.ThreadID(th) }),
		Procs: each(x.GetProcs(), func(l *ProcLane) model.ProcLane {
			return model.ProcLane{Proc: model.ProcID(l.GetProc()), Spans: each(l.GetSpans(), func(s *SchedSpan) model.SchedSpan {
				return model.SchedSpan{
					Goroutine: model.GoroutineID(s.GetGoroutine()), Thread: model.ThreadID(s.GetThread()),
					Start: time.Duration(s.GetStartNs()), End: time.Duration(s.GetEndNs()),
				}
			})}
		}),
		Events: each(x.GetEvents(), (*SchedEvent).model),
		GC: each(x.GetGc(), func(r *GCRange) model.GCRange {
			return model.GCRange{
				Kind: gcKinds.from(r.GetKind()), Goroutine: model.GoroutineID(r.GetGoroutine()), Reason: r.GetReason(),
				Start: time.Duration(r.GetStartNs()), End: time.Duration(r.GetEndNs()),
			}
		}),
		Heap: each(x.GetHeap(), func(h *HeapSample) model.HeapSample {
			return model.HeapSample{At: time.Duration(h.GetAtNs()), ObjectsBytes: h.GetObjectsBytes(), GoalBytes: h.GetGoalBytes()}
		}),
		GCEnds: each(x.GetGcEnds(), func(e *GCEnd) model.GCEnd {
			return model.GCEnd{At: time.Duration(e.GetAtNs()), LiveHeapBytes: e.GetLiveHeapBytes(), HeapGoalBytes: e.GetHeapGoalBytes()}
		}),
	}
	if entries := x.GetEntries(); len(entries) > 0 {
		out.Entries = make(map[model.GoroutineID]string, len(entries))
		for g, e := range entries {
			out.Entries[model.GoroutineID(g)] = e
		}
	}
	return out
}

func newEvent(e model.SchedEvent) *SchedEvent {
	return &SchedEvent{
		AtNs: int64(e.At), Kind: eventKinds.to(e.Kind), Goroutine: int64(e.Goroutine),
		Place: &GoroutinePlace{
			Kind: placeKinds.to(e.Place.Kind), Proc: int64(e.Place.Proc), HasProc: e.Place.HasProc, Thread: int64(e.Place.Thread),
			Reason: e.Place.Reason, Cgo: e.Place.Cgo,
		},
		Function: e.Function, Location: e.Location, Created: e.Created,
		WokenBy: int64(e.WokenBy), WokenByFunction: e.WokenByFunction, WokenByLocation: e.WokenByLocation,
		Proc: int64(e.Proc), ProcStatus: procStatuses.to(e.ProcStatus), Thread: int64(e.Thread),
	}
}

func (x *SchedEvent) model() model.SchedEvent {
	p := x.GetPlace()
	return model.SchedEvent{
		At: time.Duration(x.GetAtNs()), Kind: eventKinds.from(x.GetKind()), Goroutine: model.GoroutineID(x.GetGoroutine()),
		Place: model.GoroutinePlace{
			Kind: placeKinds.from(p.GetKind()), Proc: model.ProcID(p.GetProc()), HasProc: p.GetHasProc(), Thread: model.ThreadID(p.GetThread()),
			Reason: p.GetReason(), Cgo: p.GetCgo(),
		},
		Function: x.GetFunction(), Location: x.GetLocation(), Created: x.GetCreated(),
		WokenBy: model.GoroutineID(x.GetWokenBy()), WokenByFunction: x.GetWokenByFunction(), WokenByLocation: x.GetWokenByLocation(),
		Proc: model.ProcID(x.GetProc()), ProcStatus: procStatuses.from(x.GetProcStatus()), Thread: model.ThreadID(x.GetThread()),
	}
}

func newStats(s model.TraceStats) *TraceStats {
	return &TraceStats{
		WindowNs: int64(s.Window), Procs: int64(s.Procs), RunnableNs: int64(s.Runnable),
		Waits: each(s.Waits, func(w model.WaitSite) *WaitSite {
			return &WaitSite{
				Kind: placeKinds.to(w.Kind), Reason: w.Reason, Cgo: w.Cgo, Entry: w.Entry, Function: w.Function, Location: w.Location,
				Count: int64(w.Count), TotalNs: int64(w.Total), MaxNs: int64(w.Max),
			}
		}),
		SchedLatency: newHistogram(s.SchedLatency), Stw: newHistogram(s.STW), GcMaxShare: s.GCMaxShare,
		ProcRunningNs: int64(s.ProcRunning), ProcGcNs: int64(s.ProcGC),
	}
}

func (x *TraceStats) model() model.TraceStats {
	return model.TraceStats{
		Window: time.Duration(x.GetWindowNs()), Procs: int(x.GetProcs()), Runnable: time.Duration(x.GetRunnableNs()),
		Waits: each(x.GetWaits(), func(w *WaitSite) model.WaitSite {
			return model.WaitSite{
				Kind: placeKinds.from(w.GetKind()), Reason: w.GetReason(), Cgo: w.GetCgo(), Entry: w.GetEntry(), Function: w.GetFunction(),
				Location: w.GetLocation(), Count: int(w.GetCount()), Total: time.Duration(w.GetTotalNs()), Max: time.Duration(w.GetMaxNs()),
			}
		}),
		SchedLatency: x.GetSchedLatency().model(), STW: x.GetStw().model(), GCMaxShare: x.GetGcMaxShare(),
		ProcRunning: time.Duration(x.GetProcRunningNs()), ProcGC: time.Duration(x.GetProcGcNs()),
	}
}

func newHistogram(h model.Histogram) *Histogram {
	return &Histogram{Counts: h.Counts, MaxNs: int64(h.Max)}
}

func (x *Histogram) model() model.Histogram {
	return model.Histogram{Counts: x.GetCounts(), Max: time.Duration(x.GetMaxNs())}
}

// Timestamp は時刻がないとき (ゼロ値) は送らない。受け取った側は TimeOf でゼロ値に戻す (nil の AsTime は 1970 年になる)。
// 受け取った時刻は server のローカル時刻にする (AsTime は UTC を返し、API の JSON の時刻が他とずれる)
func Timestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func TimeOf(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime().Local()
}

// each は空のときに nil を返す。受け取った側で空と nil を区別しないように、送る側と同じ形に揃える
func each[A, B any](in []A, f func(A) B) []B {
	if len(in) == 0 {
		return nil
	}
	out := make([]B, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

// enum は model の文字列の値と proto の enum の対応。対応にない値は、どちらの向きでもゼロ値 (未指定) にする
type enum[M comparable, P comparable] struct {
	toProto   map[M]P
	fromProto map[P]M
}

func newEnum[M comparable, P comparable](pairs map[M]P) enum[M, P] {
	e := enum[M, P]{toProto: pairs, fromProto: make(map[P]M, len(pairs))}
	for m, p := range pairs {
		e.fromProto[p] = m
	}
	return e
}

func (e enum[M, P]) to(m M) P   { return e.toProto[m] }
func (e enum[M, P]) from(p P) M { return e.fromProto[p] }

var (
	logLevels = newEnum(map[model.LogLevel]LogLevel{
		model.LogLevelDebug: LogLevel_LOG_LEVEL_DEBUG,
		model.LogLevelInfo:  LogLevel_LOG_LEVEL_INFO,
		model.LogLevelWarn:  LogLevel_LOG_LEVEL_WARN,
		model.LogLevelError: LogLevel_LOG_LEVEL_ERROR,
	})
	leakChecks = newEnum(map[model.LeakCheckStatus]LeakCheckStatus{
		model.LeakCheckDone:        LeakCheckStatus_LEAK_CHECK_STATUS_DONE,
		model.LeakCheckSkipped:     LeakCheckStatus_LEAK_CHECK_STATUS_SKIPPED,
		model.LeakCheckUnsupported: LeakCheckStatus_LEAK_CHECK_STATUS_UNSUPPORTED,
		model.LeakCheckDisabled:    LeakCheckStatus_LEAK_CHECK_STATUS_DISABLED,
	})
	eventKinds = newEnum(map[model.SchedEventKind]SchedEventKind{
		model.SchedEventGoroutine: SchedEventKind_SCHED_EVENT_KIND_GOROUTINE,
		model.SchedEventProc:      SchedEventKind_SCHED_EVENT_KIND_PROC,
	})
	procStatuses = newEnum(map[model.ProcStatus]ProcStatus{
		model.ProcIdle:    ProcStatus_PROC_STATUS_IDLE,
		model.ProcRunning: ProcStatus_PROC_STATUS_RUNNING,
	})
	placeKinds = newEnum(map[model.GoroutinePlaceKind]GoroutinePlaceKind{
		model.PlaceRunning:     GoroutinePlaceKind_GOROUTINE_PLACE_KIND_RUNNING,
		model.PlaceLocalQueue:  GoroutinePlaceKind_GOROUTINE_PLACE_KIND_LOCAL_RUN_QUEUE,
		model.PlaceGlobalQueue: GoroutinePlaceKind_GOROUTINE_PLACE_KIND_GLOBAL_RUN_QUEUE,
		model.PlaceWaiting:     GoroutinePlaceKind_GOROUTINE_PLACE_KIND_WAITING,
		model.PlaceSyscall:     GoroutinePlaceKind_GOROUTINE_PLACE_KIND_SYSCALL,
		model.PlaceDead:        GoroutinePlaceKind_GOROUTINE_PLACE_KIND_DEAD,
	})
	gcKinds = newEnum(map[model.GCRangeKind]GCRangeKind{
		model.GCStopTheWorld: GCRangeKind_GC_RANGE_KIND_STOP_THE_WORLD,
		model.GCMarkAssist:   GCRangeKind_GC_RANGE_KIND_MARK_ASSIST,
		model.GCMark:         GCRangeKind_GC_RANGE_KIND_MARK,
	})
)
