package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/domain"
)

func TestSchedulerTimelineBreakdown(t *testing.T) {
	t.Parallel()
	const ms = time.Millisecond
	gEv := func(at time.Duration, g domain.GoroutineID, place domain.GoroutinePlace) domain.SchedEvent {
		return domain.SchedEvent{At: at, Kind: domain.SchedEventGoroutine, Goroutine: g, Place: place}
	}
	running := domain.GoroutinePlace{Kind: domain.PlaceRunning, Proc: 0, HasProc: true}
	runnable := domain.GoroutinePlace{Kind: domain.PlaceGlobalQueue}
	chanWait := domain.GoroutinePlace{Kind: domain.PlaceWaiting, Reason: "chan receive"}
	network := domain.GoroutinePlace{Kind: domain.PlaceWaiting, Reason: "network"}
	tl := domain.SchedulerTimeline{
		Window: 100 * ms,
		Events: []domain.SchedEvent{
			gEv(0, 1, running),
			gEv(0, 2, running),
			gEv(10*ms, 1, runnable),
			gEv(20*ms, 1, chanWait),
			gEv(60*ms, 1, network),
			gEv(90*ms, 1, running),
		},
		GC: []domain.GCRange{
			{Kind: domain.GCStopTheWorld, Start: 25 * ms, End: 30 * ms},
			{Kind: domain.GCMarkAssist, Goroutine: 1, Start: 92 * ms, End: 95 * ms},
			{Kind: domain.GCMarkAssist, Goroutine: 2, Start: 50 * ms, End: 70 * ms},
		},
	}

	tests := []struct {
		name        string
		g           domain.GoroutineID
		from, to    time.Duration
		want        map[domain.PrimaryCause]time.Duration
		wantPrimary domain.PrimaryCause
		unrecorded  time.Duration
	}{
		{
			name: "success: the whole window is split by state, and GC ranges are taken out of the overlapped state",
			g:    1, from: 0, to: 100 * ms,
			want: map[domain.PrimaryCause]time.Duration{
				domain.CauseRunning: 10*ms + 7*ms,
				domain.CauseCPUWait: 10 * ms,
				domain.CauseBlocked: 35 * ms,
				domain.CauseIOWait:  30 * ms,
				domain.CauseGC:      5*ms + 3*ms,
			},
			wantPrimary: domain.CauseBlocked,
		},
		{
			name: "success: only the requested interval is counted",
			g:    1, from: 55 * ms, to: 80 * ms,
			want:        map[domain.PrimaryCause]time.Duration{domain.CauseBlocked: 5 * ms, domain.CauseIOWait: 20 * ms},
			wantPrimary: domain.CauseIOWait,
		},
		{
			name: "success: mark assist of another goroutine is not counted",
			g:    2, from: 0, to: 100 * ms,
			want:        map[domain.PrimaryCause]time.Duration{domain.CauseRunning: 75 * ms, domain.CauseGC: 25 * ms},
			wantPrimary: domain.CauseRunning,
		},
		{
			name: "success: a goroutine without events is unrecorded",
			g:    3, from: 0, to: 100 * ms,
			want:       map[domain.PrimaryCause]time.Duration{},
			unrecorded: 100 * ms,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tl.Breakdown(tt.g, tt.from, tt.to)
			assert.Equal(t, tt.want, got.Durations)
			assert.Equal(t, tt.unrecorded, got.Unrecorded)
			primary, ok := got.Primary()
			assert.Equal(t, tt.wantPrimary != "", ok)
			assert.Equal(t, tt.wantPrimary, primary)
		})
	}
}

func TestCauseOfWaitReason(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]domain.PrimaryCause{
		"chan receive":                 domain.CauseBlocked,
		"sync.Mutex.Lock":              domain.CauseBlocked,
		"select":                       domain.CauseBlocked,
		"network":                      domain.CauseIOWait,
		"IO wait":                      domain.CauseIOWait,
		"sleep":                        domain.CauseSleep,
		"GC mark assist wait for work": domain.CauseGC,
		"wait until GC ends":           domain.CauseGC,
	} {
		assert.Equal(t, want, domain.CauseOfWaitReason(reason), reason)
	}
}

func TestSchedulerTimeline_PlaceOfCause(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	tl := domain.SchedulerTimeline{Window: 100 * ms, Events: []domain.SchedEvent{
		{At: 10 * ms, Kind: domain.SchedEventGoroutine, Goroutine: 1, Place: domain.GoroutinePlace{Kind: domain.PlaceWaiting, Reason: "sync"}, Function: "main.handle", Location: "/app/h.go:10"},
		{At: 80 * ms, Kind: domain.SchedEventGoroutine, Goroutine: 1, Place: domain.GoroutinePlace{Kind: domain.PlaceWaiting, Reason: "sleep"}, Function: "time.Sleep", Location: "/go/time.go:1"},
	}}

	got, ok := tl.PlaceOfCause(1, domain.CauseBlocked, 90*ms)

	assert.True(t, ok)
	assert.Equal(t, "/app/h.go:10", got.Location, "the place where it entered the primary state, not where it was last")
}
