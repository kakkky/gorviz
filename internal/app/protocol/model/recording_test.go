package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

func TestTraceChunk_InheritWaitReasons(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	gEv := func(at time.Duration, g model.GoroutineID, kind model.GoroutinePlaceKind, reason, fn string) model.SchedEvent {
		return model.SchedEvent{At: at, Kind: model.SchedEventGoroutine, Goroutine: g, Place: model.GoroutinePlace{Kind: kind, Reason: reason}, Function: fn}
	}
	prev := model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{Window: time.Second, Events: []model.SchedEvent{
		gEv(100*ms, 1, model.PlaceWaiting, "sleep", "time.Sleep"),
		gEv(200*ms, 2, model.PlaceRunning, "", ""),
	}}}
	next := model.TraceChunk{StartedAt: base.Add(time.Second + 10*ms), Timeline: model.SchedulerTimeline{Window: time.Second, Events: []model.SchedEvent{
		gEv(0, 1, model.PlaceWaiting, model.UnknownWaitReason, ""),
		gEv(0, 2, model.PlaceWaiting, model.UnknownWaitReason, ""),
		gEv(400*ms, 1, model.PlaceWaiting, model.UnknownWaitReason, ""),
	}}}

	next.InheritWaitReasons(prev)

	ev := next.Timeline.Events
	assert.Equal(t, "sleep", ev[0].Place.Reason, "the carried wait takes the reason from the previous chunk")
	assert.Equal(t, "time.Sleep", ev[0].Function)
	assert.Equal(t, model.UnknownWaitReason, ev[1].Place.Reason, "a goroutine that was running before is not given a reason")
	assert.Equal(t, "sleep", ev[2].Place.Reason, "a rewrite in the middle of the chunk keeps the reason")

	far := model.TraceChunk{StartedAt: base.Add(10 * time.Second), Timeline: model.SchedulerTimeline{Window: time.Second, Events: []model.SchedEvent{
		gEv(0, 1, model.PlaceWaiting, model.UnknownWaitReason, ""),
	}}}
	far.InheritWaitReasons(prev)
	assert.Equal(t, model.UnknownWaitReason, far.Timeline.Events[0].Place.Reason, "too far from the previous chunk to assume the same wait")
}

func TestJoinChunks(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	blocked := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "sync"}
	running := model.GoroutinePlace{Kind: model.PlaceRunning, HasProc: true}
	first := model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{
		Window: time.Second, Entries: map[model.GoroutineID]string{1: "main.handle"},
		Events: []model.SchedEvent{{At: 600 * ms, Kind: model.SchedEventGoroutine, Goroutine: 1, Place: blocked}},
		Procs:  []model.ProcLane{{Proc: 0, Spans: []model.SchedSpan{{Goroutine: 1, Start: 500 * ms, End: 600 * ms}}}},
	}}
	second := model.TraceChunk{StartedAt: base.Add(1010 * ms), Timeline: model.SchedulerTimeline{
		Window: time.Second,
		Events: []model.SchedEvent{
			{At: 0, Kind: model.SchedEventGoroutine, Goroutine: 1, Place: blocked},
			{At: 200 * ms, Kind: model.SchedEventGoroutine, Goroutine: 1, Place: running},
		},
		Procs: []model.ProcLane{{Proc: 0, Spans: []model.SchedSpan{{Goroutine: 1, Start: 200 * ms, End: 300 * ms}}}},
	}}

	tl, start, ok := model.JoinChunks([]model.TraceChunk{first, second}, base.Add(500*ms), base.Add(1500*ms))

	require.True(t, ok)
	assert.Equal(t, base.Add(500*ms), start)
	assert.Equal(t, time.Second, tl.Window, "the gap between the chunks is part of the window")
	var ats []time.Duration
	for _, ev := range tl.Events {
		ats = append(ats, ev.At)
	}
	assert.Contains(t, ats, 100*ms, "the first chunk is shifted to the start of the window")
	assert.Contains(t, ats, 710*ms, "the later chunk is placed at its own start")
	assert.Equal(t, model.GoroutineID(1), tl.StateAt(800*ms).Procs[0].Running, "the state after the join is replayed from the later chunk")
	assert.Equal(t, "main.handle", tl.Entries[1])
	require.Len(t, tl.Procs, 1, "lanes of the same P are merged")
	assert.Len(t, tl.Procs[0].Spans, 2)
}
