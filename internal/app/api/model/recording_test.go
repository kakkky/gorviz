package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/api/model"
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
