package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

func TestSchedulerTimeline_StateAt(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	gEv := func(at time.Duration, g model.GoroutineID, place model.GoroutinePlace) model.SchedEvent {
		return model.SchedEvent{At: at, Kind: model.SchedEventGoroutine, Goroutine: g, Place: place}
	}
	pEv := func(at time.Duration, p model.ProcID, status model.ProcStatus, m model.ThreadID) model.SchedEvent {
		return model.SchedEvent{At: at, Kind: model.SchedEventProc, Proc: p, ProcStatus: status, Thread: m}
	}
	lrq := func(p model.ProcID) model.GoroutinePlace {
		return model.GoroutinePlace{Kind: model.PlaceLocalQueue, Proc: p, HasProc: true}
	}
	runningOn := func(p model.ProcID, m model.ThreadID) model.GoroutinePlace {
		return model.GoroutinePlace{Kind: model.PlaceRunning, Proc: p, HasProc: true, Thread: m}
	}
	syscallOn := func(p model.ProcID, m model.ThreadID) model.GoroutinePlace {
		return model.GoroutinePlace{Kind: model.PlaceSyscall, Proc: p, HasProc: true, Thread: m}
	}

	tl := model.SchedulerTimeline{
		Window:  10 * ms,
		Procs:   []model.ProcLane{{Proc: 0}, {Proc: 1}},
		Entries: map[model.GoroutineID]string{1: "main.worker"},
		Events: []model.SchedEvent{
			pEv(0, 0, model.ProcRunning, 0),
			gEv(0, 1, runningOn(0, 0)),
			gEv(1*ms, 2, lrq(0)),
			gEv(2*ms, 3, lrq(0)),
			gEv(3*ms, 1, model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "chan receive"}),
			gEv(3*ms, 2, runningOn(0, 0)),
			gEv(4*ms, 2, model.GoroutinePlace{Kind: model.PlaceGlobalQueue}),
			gEv(4*ms, 3, runningOn(0, 0)),
			gEv(5*ms, 3, syscallOn(0, 0)),
			pEv(6*ms, 0, model.ProcIdle, 0),
			pEv(6*ms, 1, model.ProcRunning, 1),
			gEv(6*ms, 2, runningOn(1, 1)),
			gEv(7*ms, 2, model.GoroutinePlace{Kind: model.PlaceDead}),
		},
	}

	tests := []struct {
		name string
		at   time.Duration
		want model.SchedState
	}{
		{
			name: "success: readied goroutines queue on the P's local run queue in order",
			at:   2 * ms,
			want: model.SchedState{
				Procs: []model.ProcSnapshot{
					{Proc: 0, Status: model.ProcRunning, Thread: 0, HasThread: true, Running: 1, HasRunning: true, LocalQueue: []model.GoroutineID{2, 3}},
					{Proc: 1, Status: model.ProcIdle, LocalQueue: []model.GoroutineID{}},
				},
				GlobalQueue: []model.GoroutineID{},
				Waiting:     map[model.WaitingGroup][]model.GoroutineID{},
			},
		},
		{
			name: "success: blocked goroutine is grouped by its entry function and wait reason, and preempted one goes to the global run queue",
			at:   4 * ms,
			want: model.SchedState{
				Procs: []model.ProcSnapshot{
					{Proc: 0, Status: model.ProcRunning, Thread: 0, HasThread: true, Running: 3, HasRunning: true, LocalQueue: []model.GoroutineID{}},
					{Proc: 1, Status: model.ProcIdle, LocalQueue: []model.GoroutineID{}},
				},
				GlobalQueue: []model.GoroutineID{2},
				Waiting:     map[model.WaitingGroup][]model.GoroutineID{{Entry: "main.worker", Reason: "chan receive"}: {1}},
			},
		},
		{
			name: "success: goroutine in syscall stays on its running P",
			at:   5 * ms,
			want: model.SchedState{
				Procs: []model.ProcSnapshot{
					{Proc: 0, Status: model.ProcRunning, Thread: 0, HasThread: true, Running: 3, HasRunning: true, RunningInSyscall: true, LocalQueue: []model.GoroutineID{}},
					{Proc: 1, Status: model.ProcIdle, LocalQueue: []model.GoroutineID{}},
				},
				GlobalQueue: []model.GoroutineID{2},
				Waiting:     map[model.WaitingGroup][]model.GoroutineID{{Entry: "main.worker", Reason: "chan receive"}: {1}},
			},
		},
		{
			name: "success: syscall goroutine whose P went idle is handed off, and exited goroutine is dead",
			at:   7 * ms,
			want: model.SchedState{
				Procs: []model.ProcSnapshot{
					{Proc: 0, Status: model.ProcIdle, Thread: 0, LocalQueue: []model.GoroutineID{}},
					{Proc: 1, Status: model.ProcRunning, Thread: 1, HasThread: true, LocalQueue: []model.GoroutineID{}},
				},
				GlobalQueue: []model.GoroutineID{},
				Waiting:     map[model.WaitingGroup][]model.GoroutineID{{Entry: "main.worker", Reason: "chan receive"}: {1}},
				HandedOff:   []model.SyscallGoroutine{{Goroutine: 3, Thread: 0}},
				Dead:        []model.GoroutineID{2},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tl.StateAt(tt.at))
		})
	}
}

func TestSchedulerTimeline_Slice(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	gEv := func(at time.Duration, g model.GoroutineID, place model.GoroutinePlace) model.SchedEvent {
		return model.SchedEvent{At: at, Kind: model.SchedEventGoroutine, Goroutine: g, Place: place}
	}
	lrq := model.GoroutinePlace{Kind: model.PlaceLocalQueue, Proc: 0, HasProc: true}
	running := model.GoroutinePlace{Kind: model.PlaceRunning, Proc: 0, HasProc: true}
	dead := model.GoroutinePlace{Kind: model.PlaceDead}
	created := func(e model.SchedEvent) model.SchedEvent { e.Created = true; return e }
	pRunning := model.SchedEvent{At: 0, Kind: model.SchedEventProc, Proc: 0, ProcStatus: model.ProcRunning}

	tl := model.SchedulerTimeline{
		Window: 10 * ms,
		Procs: []model.ProcLane{{Proc: 0, Spans: []model.SchedSpan{
			{Goroutine: 1, Start: 0, End: 4 * ms},
			{Goroutine: 2, Start: 4 * ms, End: 9 * ms},
		}}},
		Entries: map[model.GoroutineID]string{1: "main.a"},
		Events: []model.SchedEvent{
			pRunning,
			gEv(0, 1, running),
			gEv(1*ms, 2, lrq),
			gEv(2*ms, 3, lrq),
			gEv(4*ms, 1, dead),
			gEv(4*ms, 2, running),
			created(gEv(4500*time.Microsecond, 4, lrq)),
			gEv(9*ms, 2, dead),
		},
	}

	got := tl.Slice(3*ms, 5*ms)

	assert.Equal(t, 2*ms, got.Window)
	assert.Equal(t, []model.SchedEvent{
		pRunning,
		gEv(0, 1, running),
		gEv(0, 2, lrq),
		gEv(0, 3, lrq),
		gEv(1*ms, 1, dead),
		gEv(1*ms, 2, running),
		created(gEv(1500*time.Microsecond, 4, lrq)),
	}, got.Events, "state at 3ms is carried as t=0 events keeping queue order, and later events are shifted")
	assert.Equal(t, []model.ProcLane{{Proc: 0, Spans: []model.SchedSpan{
		{Goroutine: 1, Start: 0, End: 1 * ms},
		{Goroutine: 2, Start: 1 * ms, End: 2 * ms},
	}}}, got.Procs)
	assert.Equal(t, 1, got.GoroutinesDestroyed)
	assert.Equal(t, 1, got.GoroutinesCreated, "goroutines created inside the slice are counted")
	assert.Equal(t, tl.StateAt(5*ms).GlobalQueue, got.StateAt(2*ms).GlobalQueue)
	assert.Equal(t, tl.StateAt(5*ms).Procs, got.StateAt(2*ms).Procs)
}

func TestTraceChunk_Since(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := model.TraceChunk{
		StartedAt: base,
		Timeline: model.SchedulerTimeline{Window: 10 * ms, Events: []model.SchedEvent{
			{At: 2 * ms, Kind: model.SchedEventGoroutine, Goroutine: 1, Place: model.GoroutinePlace{Kind: model.PlaceGlobalQueue}},
			{At: 8 * ms, Kind: model.SchedEventGoroutine, Goroutine: 1, Place: model.GoroutinePlace{Kind: model.PlaceDead}},
		}},
		LogMarks: []model.TraceLogMark{{Seq: 1, At: 3 * ms}, {Seq: 2, At: 7 * ms}},
	}

	got := c.Since(base.Add(5 * ms))

	assert.Equal(t, base.Add(5*ms), got.StartedAt)
	assert.Equal(t, 5*ms, got.Timeline.Window)
	assert.Equal(t, []model.TraceLogMark{{Seq: 2, At: 2 * ms}}, got.LogMarks, "marks before the cut are dropped and later ones are shifted")
	assert.Equal(t, model.PlaceGlobalQueue, got.Timeline.Events[0].Place.Kind, "the state at the cut is carried in")
}
