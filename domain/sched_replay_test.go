package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/domain"
)

func TestSchedulerTimeline_StateAt(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	gEv := func(at time.Duration, g domain.GoroutineID, place domain.GoroutinePlace) domain.SchedEvent {
		return domain.SchedEvent{At: at, Kind: domain.SchedEventGoroutine, Goroutine: g, Place: place}
	}
	pEv := func(at time.Duration, p domain.ProcID, status domain.ProcStatus, m domain.ThreadID) domain.SchedEvent {
		return domain.SchedEvent{At: at, Kind: domain.SchedEventProc, Proc: p, ProcStatus: status, Thread: m}
	}
	lrq := func(p domain.ProcID) domain.GoroutinePlace {
		return domain.GoroutinePlace{Kind: domain.PlaceLocalQueue, Proc: p, HasProc: true}
	}
	runningOn := func(p domain.ProcID, m domain.ThreadID) domain.GoroutinePlace {
		return domain.GoroutinePlace{Kind: domain.PlaceRunning, Proc: p, HasProc: true, Thread: m}
	}
	syscallOn := func(p domain.ProcID, m domain.ThreadID) domain.GoroutinePlace {
		return domain.GoroutinePlace{Kind: domain.PlaceSyscall, Proc: p, HasProc: true, Thread: m}
	}

	tl := domain.SchedulerTimeline{
		Window:  10 * ms,
		Procs:   []domain.ProcLane{{Proc: 0}, {Proc: 1}},
		Entries: map[domain.GoroutineID]string{1: "main.worker"},
		Events: []domain.SchedEvent{
			pEv(0, 0, domain.ProcRunning, 0),
			gEv(0, 1, runningOn(0, 0)),
			gEv(1*ms, 2, lrq(0)),
			gEv(2*ms, 3, lrq(0)),
			gEv(3*ms, 1, domain.GoroutinePlace{Kind: domain.PlaceWaiting, Reason: "chan receive"}),
			gEv(3*ms, 2, runningOn(0, 0)),
			gEv(4*ms, 2, domain.GoroutinePlace{Kind: domain.PlaceGlobalQueue}),
			gEv(4*ms, 3, runningOn(0, 0)),
			gEv(5*ms, 3, syscallOn(0, 0)),
			pEv(6*ms, 0, domain.ProcIdle, 0),
			pEv(6*ms, 1, domain.ProcRunning, 1),
			gEv(6*ms, 2, runningOn(1, 1)),
			gEv(7*ms, 2, domain.GoroutinePlace{Kind: domain.PlaceDead}),
		},
	}

	tests := []struct {
		name string
		at   time.Duration
		want domain.SchedState
	}{
		{
			name: "success: readied goroutines queue on the P's LRQ in order",
			at:   2 * ms,
			want: domain.SchedState{
				Procs: []domain.ProcSnapshot{
					{Proc: 0, Status: domain.ProcRunning, Thread: 0, HasThread: true, Running: 1, HasRunning: true, LocalQueue: []domain.GoroutineID{2, 3}},
					{Proc: 1, Status: domain.ProcIdle, LocalQueue: []domain.GoroutineID{}},
				},
				GlobalQueue: []domain.GoroutineID{},
				Waiting:     map[domain.WaitingGroup][]domain.GoroutineID{},
			},
		},
		{
			name: "success: blocked goroutine is grouped by its entry function and wait reason, and preempted one goes to GRQ",
			at:   4 * ms,
			want: domain.SchedState{
				Procs: []domain.ProcSnapshot{
					{Proc: 0, Status: domain.ProcRunning, Thread: 0, HasThread: true, Running: 3, HasRunning: true, LocalQueue: []domain.GoroutineID{}},
					{Proc: 1, Status: domain.ProcIdle, LocalQueue: []domain.GoroutineID{}},
				},
				GlobalQueue: []domain.GoroutineID{2},
				Waiting:     map[domain.WaitingGroup][]domain.GoroutineID{{Entry: "main.worker", Reason: "chan receive"}: {1}},
			},
		},
		{
			name: "success: P whose goroutine is in syscall is shown as syscall",
			at:   5 * ms,
			want: domain.SchedState{
				Procs: []domain.ProcSnapshot{
					{Proc: 0, Status: domain.ProcSyscall, Thread: 0, HasThread: true, Running: 3, HasRunning: true, LocalQueue: []domain.GoroutineID{}},
					{Proc: 1, Status: domain.ProcIdle, LocalQueue: []domain.GoroutineID{}},
				},
				GlobalQueue: []domain.GoroutineID{2},
				Waiting:     map[domain.WaitingGroup][]domain.GoroutineID{{Entry: "main.worker", Reason: "chan receive"}: {1}},
			},
		},
		{
			name: "success: syscall goroutine whose P went idle is handed off, and exited goroutine is dead",
			at:   7 * ms,
			want: domain.SchedState{
				Procs: []domain.ProcSnapshot{
					{Proc: 0, Status: domain.ProcIdle, Thread: 0, LocalQueue: []domain.GoroutineID{}},
					{Proc: 1, Status: domain.ProcRunning, Thread: 1, HasThread: true, LocalQueue: []domain.GoroutineID{}},
				},
				GlobalQueue: []domain.GoroutineID{},
				Waiting:     map[domain.WaitingGroup][]domain.GoroutineID{{Entry: "main.worker", Reason: "chan receive"}: {1}},
				HandedOff:   []domain.SyscallGoroutine{{Goroutine: 3, Thread: 0}},
				Dead:        []domain.GoroutineID{2},
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

func TestSchedulerTimeline_EnrichFromDump(t *testing.T) {
	t.Parallel()
	waitingUnknown := domain.GoroutinePlace{Kind: domain.PlaceWaiting, Reason: domain.UnknownWaitReason}
	tl := domain.SchedulerTimeline{
		Entries: map[domain.GoroutineID]string{1: "main.truncated", 3: "main.fromTrace"},
		Events: []domain.SchedEvent{
			{Kind: domain.SchedEventGoroutine, Goroutine: 1, Place: waitingUnknown},
			{Kind: domain.SchedEventGoroutine, Goroutine: 2, Place: waitingUnknown},
			{Kind: domain.SchedEventGoroutine, Goroutine: 3, Place: waitingUnknown},
		},
	}
	tl.EnrichFromDump([]domain.Goroutine{
		{ID: 1, State: "chan receive", EntryFunction: "main.worker", CurrentFunction: "main.fetch"},
		{ID: 2, State: "running", EntryFunction: "main.busy", CurrentFunction: "main.busy"},
	})

	assert.Equal(t, map[domain.GoroutineID]string{1: "main.worker", 2: "main.busy", 3: "main.fromTrace"}, tl.Entries)
	assert.Equal(t, []domain.SchedEvent{
		{Kind: domain.SchedEventGoroutine, Goroutine: 1, Place: domain.GoroutinePlace{Kind: domain.PlaceWaiting, Reason: "chan receive"}, Function: "main.fetch"},
		{Kind: domain.SchedEventGoroutine, Goroutine: 2, Place: waitingUnknown},
		{Kind: domain.SchedEventGoroutine, Goroutine: 3, Place: waitingUnknown},
	}, tl.Events)
}

func TestSchedulerTimeline_Slice(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	gEv := func(at time.Duration, g domain.GoroutineID, place domain.GoroutinePlace) domain.SchedEvent {
		return domain.SchedEvent{At: at, Kind: domain.SchedEventGoroutine, Goroutine: g, Place: place}
	}
	lrq := domain.GoroutinePlace{Kind: domain.PlaceLocalQueue, Proc: 0, HasProc: true}
	running := domain.GoroutinePlace{Kind: domain.PlaceRunning, Proc: 0, HasProc: true}
	dead := domain.GoroutinePlace{Kind: domain.PlaceDead}
	pRunning := domain.SchedEvent{At: 0, Kind: domain.SchedEventProc, Proc: 0, ProcStatus: domain.ProcRunning}

	tl := domain.SchedulerTimeline{
		Window: 10 * ms,
		Procs: []domain.ProcLane{{Proc: 0, Spans: []domain.SchedSpan{
			{Goroutine: 1, Start: 0, End: 4 * ms},
			{Goroutine: 2, Start: 4 * ms, End: 9 * ms},
		}}},
		Entries: map[domain.GoroutineID]string{1: "main.a"},
		Events: []domain.SchedEvent{
			pRunning,
			gEv(0, 1, running),
			gEv(1*ms, 2, lrq),
			gEv(2*ms, 3, lrq),
			gEv(4*ms, 1, dead),
			gEv(4*ms, 2, running),
			gEv(9*ms, 2, dead),
		},
	}

	got := tl.Slice(3*ms, 5*ms)

	assert.Equal(t, 2*ms, got.Window)
	assert.Equal(t, []domain.SchedEvent{
		pRunning,
		gEv(0, 1, running),
		gEv(0, 2, lrq),
		gEv(0, 3, lrq),
		gEv(1*ms, 1, dead),
		gEv(1*ms, 2, running),
	}, got.Events, "state at 3ms is carried as t=0 events keeping queue order, and later events are shifted")
	assert.Equal(t, []domain.ProcLane{{Proc: 0, Spans: []domain.SchedSpan{
		{Goroutine: 1, Start: 0, End: 1 * ms},
		{Goroutine: 2, Start: 1 * ms, End: 2 * ms},
	}}}, got.Procs)
	assert.Equal(t, 1, got.GoroutinesDestroyed)
	assert.Equal(t, tl.StateAt(5*ms).GlobalQueue, got.StateAt(2*ms).GlobalQueue)
	assert.Equal(t, tl.StateAt(5*ms).Procs, got.StateAt(2*ms).Procs)
}
