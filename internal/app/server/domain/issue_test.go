package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestMessageShape(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		domain.MessageShape("job 42 failed for user 3f2b1c9e-1a2b-4c3d-8e9f-0123456789ab at 0xc000123"),
		domain.MessageShape("job 7 failed for user 00000000-0000-4000-8000-000000000000 at 0xdeadbeef"),
		"numbers and ids are masked so that the same kind of error has the same shape")
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

func TestDetectRuntimeIssues_CPUWait(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	stats := model.TraceStats{Window: time.Second, Runnable: 1500 * time.Millisecond}
	for range 100 {
		stats.SchedLatency.Observe(30 * time.Millisecond)
	}
	chunk := model.TraceChunk{StartedAt: start, Timeline: model.SchedulerTimeline{Window: time.Second}, Stats: stats}

	issues := domain.DetectRuntimeIssues(chunk, domain.IssuePolicy{CPUWaitRatio: 1})

	require.Len(t, issues, 1)
	assert.Equal(t, model.IssueCPUWaitSpike, issues[0].Kind)
	assert.Equal(t, "1.5 goroutines waiting for a P on average, p99 wait to run 30ms", issues[0].Summary, "measured from the counts over all events, with the delay it caused")
}

func TestFormatShortDuration(t *testing.T) {
	t.Parallel()
	for d, want := range map[time.Duration]string{
		990 * time.Millisecond:   "990ms",
		38600 * time.Microsecond: "39ms",
		1234 * time.Millisecond:  "1.2s",
		250 * time.Microsecond:   "250µs",
		1500 * time.Nanosecond:   "1.5µs",
		0:                        "0µs",
	} {
		assert.Equal(t, want, domain.FormatShortDuration(d), d.String())
	}
}
