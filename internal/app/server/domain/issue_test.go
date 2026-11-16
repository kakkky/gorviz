package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestMessageShape(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		domain.MessageShape("job 42 failed for user 3f2b1c9e-1a2b-4c3d-8e9f-0123456789ab at 0xc000123"),
		domain.MessageShape("job 7 failed for user 00000000-0000-4000-8000-000000000000 at 0xdeadbeef"),
		"numbers and ids are masked so that the same kind of error has the same shape")
}

func TestSchedulerTimeline_TotalByCause(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	gEv := func(at time.Duration, g model.GoroutineID, kind model.GoroutinePlaceKind) model.SchedEvent {
		return model.SchedEvent{At: at, Kind: model.SchedEventGoroutine, Goroutine: g, Place: model.GoroutinePlace{Kind: kind}}
	}
	tl := model.SchedulerTimeline{Window: 10 * ms, Events: []model.SchedEvent{
		gEv(0, 1, model.PlaceGlobalQueue), gEv(0, 2, model.PlaceGlobalQueue), gEv(4*ms, 1, model.PlaceRunning),
	}}

	got := domain.TotalByCause(tl)

	assert.Equal(t, 4*ms+10*ms, got[domain.CauseCPUWait])
	assert.Equal(t, 6*ms, got[domain.CauseRunning])
}

func TestDetectRuntimeIssues(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	policy := domain.IssuePolicy{CPUWaitRatio: 1, GCPerSecond: 5}
	chunk := func(tl model.SchedulerTimeline) model.TraceChunk {
		tl.Window = time.Second
		return model.TraceChunk{StartedAt: start, Timeline: tl}
	}

	tests := []struct {
		name string
		tl   model.SchedulerTimeline
		want []domain.Certainty
	}{
		{name: "success: frequent GC is a fact", tl: model.SchedulerTimeline{GCCycles: 6}, want: []domain.Certainty{domain.CertaintyFact}},
		{
			name: "success: heap near the goal is not an issue (the heap always grows toward the goal before a GC)",
			tl:   model.SchedulerTimeline{GCCycles: 1, Heap: []model.HeapSample{{ObjectsBytes: 95, GoalBytes: 100}, {ObjectsBytes: 92, GoalBytes: 100}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []domain.Certainty
			for _, i := range domain.DetectRuntimeIssues(chunk(tt.tl), policy) {
				assert.Equal(t, model.IssueMemoryPressure, i.Kind)
				got = append(got, i.Certainty)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
