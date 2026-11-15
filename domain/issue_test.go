package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/domain"
)

func TestMessageShape(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		domain.MessageShape("job 42 failed for user 3f2b1c9e-1a2b-4c3d-8e9f-0123456789ab at 0xc000123"),
		domain.MessageShape("job 7 failed for user 00000000-0000-4000-8000-000000000000 at 0xdeadbeef"),
		"numbers and ids are masked so that the same kind of error has the same shape")
}

func TestDetectErrorSpikes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	policy := domain.IssuePolicy{ErrorSpikePerMinute: 5}
	errLine := func(ago time.Duration, msg string) domain.LogLine {
		return domain.LogLine{At: now.Add(-ago), Level: domain.LogLevelError, Message: msg}
	}
	noLocate := func(l domain.LogLine) (domain.GoroutineID, time.Time, bool) { return 0, l.At, false }

	tests := []struct {
		name  string
		lines []domain.LogLine
		want  []domain.Certainty
	}{
		{
			name: "success: over the fixed threshold in the last minute is a fact",
			lines: []domain.LogLine{
				errLine(10*time.Second, "job 1 failed"), errLine(20*time.Second, "job 2 failed"), errLine(30*time.Second, "job 3 failed"),
				errLine(40*time.Second, "job 4 failed"), errLine(50*time.Second, "job 5 failed"),
			},
			want: []domain.Certainty{domain.CertaintyFact},
		},
		{
			name: "success: under the threshold but far above the usual rate is an inference",
			lines: []domain.LogLine{
				errLine(9*time.Minute, "job 1 failed"),
				errLine(10*time.Second, "job 2 failed"), errLine(20*time.Second, "job 3 failed"), errLine(30*time.Second, "job 4 failed"),
			},
			want: []domain.Certainty{domain.CertaintyInference},
		},
		{
			name:  "success: errors as usual are not an issue",
			lines: []domain.LogLine{errLine(5*time.Minute, "job 1 failed"), errLine(3*time.Minute, "job 2 failed"), errLine(10*time.Second, "job 3 failed")},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.DetectErrorSpikes(tt.lines, now, policy, noLocate)
			var certainties []domain.Certainty
			for _, i := range got {
				assert.Equal(t, domain.IssueErrorSpike, i.Kind)
				certainties = append(certainties, i.Certainty)
			}
			assert.Equal(t, tt.want, certainties)
		})
	}
}

func TestDetectSlowRequests(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	line := func(d time.Duration) domain.LogLine {
		return domain.LogLine{At: now.Add(-time.Second), Message: "request done", Duration: d, HasDuration: true}
	}
	locate := func(l domain.LogLine) (domain.GoroutineID, time.Time, bool) { return 7, l.At, true }

	got := domain.DetectSlowRequests([]domain.LogLine{line(200 * time.Millisecond), line(1500 * time.Millisecond), line(3 * time.Second)}, now, domain.IssuePolicy{SlowRequest: time.Second}, locate)

	require.Len(t, got, 1)
	assert.Equal(t, domain.IssueSlowRequest, got[0].Kind)
	assert.Equal(t, domain.CertaintyFact, got[0].Certainty)
	assert.Equal(t, "2 requests over 1s, slowest 3s", got[0].Summary)
	assert.Equal(t, domain.GoroutineID(7), got[0].Goroutine)
	assert.Equal(t, now.Add(-time.Second-3*time.Second), got[0].From, "the slowest request is opened over its whole interval")
}

func TestSchedulerTimeline_TotalByCause(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	gEv := func(at time.Duration, g domain.GoroutineID, kind domain.GoroutinePlaceKind) domain.SchedEvent {
		return domain.SchedEvent{At: at, Kind: domain.SchedEventGoroutine, Goroutine: g, Place: domain.GoroutinePlace{Kind: kind}}
	}
	tl := domain.SchedulerTimeline{Window: 10 * ms, Events: []domain.SchedEvent{
		gEv(0, 1, domain.PlaceGlobalQueue), gEv(0, 2, domain.PlaceGlobalQueue), gEv(4*ms, 1, domain.PlaceRunning),
	}}

	got := tl.TotalByCause()

	assert.Equal(t, 4*ms+10*ms, got[domain.CauseCPUWait])
	assert.Equal(t, 6*ms, got[domain.CauseRunning])
}
