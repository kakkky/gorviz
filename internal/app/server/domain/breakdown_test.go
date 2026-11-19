package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestSchedulerTimelineBreakdown(t *testing.T) {
	t.Parallel()
	const ms = time.Millisecond
	gEv := func(at time.Duration, g model.GoroutineID, place model.GoroutinePlace) model.SchedEvent {
		return model.SchedEvent{At: at, Kind: model.SchedEventGoroutine, Goroutine: g, Place: place}
	}
	running := model.GoroutinePlace{Kind: model.PlaceRunning, Proc: 0, HasProc: true}
	runnable := model.GoroutinePlace{Kind: model.PlaceGlobalQueue}
	chanWait := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "chan receive"}
	network := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "network"}
	tl := model.SchedulerTimeline{
		Window: 100 * ms,
		Events: []model.SchedEvent{
			gEv(0, 1, running),
			gEv(0, 2, running),
			gEv(10*ms, 1, runnable),
			gEv(20*ms, 1, chanWait),
			gEv(60*ms, 1, network),
			gEv(90*ms, 1, running),
		},
		GC: []model.GCRange{
			{Kind: model.GCStopTheWorld, Reason: "GC sweep termination", Start: 25 * ms, End: 30 * ms},
			// GC 以外の STW (gorviz の goroutine dump など) は GC に数えない
			{Kind: model.GCStopTheWorld, Reason: "goroutine profile", Start: 40 * ms, End: 45 * ms},
			{Kind: model.GCMarkAssist, Goroutine: 1, Start: 92 * ms, End: 95 * ms},
			{Kind: model.GCMarkAssist, Goroutine: 2, Start: 50 * ms, End: 70 * ms},
		},
	}

	cgo := model.GoroutinePlace{Kind: model.PlaceSyscall, HasProc: true, Cgo: true}
	tl.Events = append(tl.Events, gEv(0, 4, cgo))
	tests := []struct {
		name        string
		g           model.GoroutineID
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
			name: "success: a cgo call is counted as CGO, not IO_WAIT",
			g:    4, from: 0, to: 100 * ms,
			want:        map[domain.PrimaryCause]time.Duration{domain.CauseCGO: 95 * ms, domain.CauseGC: 5 * ms},
			wantPrimary: domain.CauseCGO,
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
			got := domain.Breakdown(tl, tt.g, tt.from, tt.to)
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
	tl := model.SchedulerTimeline{Window: 100 * ms, Events: []model.SchedEvent{
		{At: 10 * ms, Kind: model.SchedEventGoroutine, Goroutine: 1, Place: model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "sync"}, Function: "main.handle", Location: "/app/h.go:10"},
		{At: 80 * ms, Kind: model.SchedEventGoroutine, Goroutine: 1, Place: model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "sleep"}, Function: "time.Sleep", Location: "/go/time.go:1"},
	}}

	got, ok := domain.PlaceOfCause(tl, 1, domain.CauseBlocked, 90*ms)

	assert.True(t, ok)
	assert.Equal(t, "/app/h.go:10", got.Location, "the place where it entered the primary state, not where it was last")
}

func TestStateSince(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	running := model.GoroutinePlace{Kind: model.PlaceRunning, Proc: 0, HasProc: true}
	blocked := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "chan send"}
	first := model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{Window: time.Second, Events: []model.SchedEvent{
		{At: 100 * ms, Kind: model.SchedEventGoroutine, Goroutine: 7, Place: running},
		{At: 300 * ms, Kind: model.SchedEventGoroutine, Goroutine: 7, Place: blocked},
	}}}
	// 次の区切りは、先頭に持ち越した状態と途中の状態の書き直しがあるだけで、G は止まったまま
	second := model.TraceChunk{StartedAt: base.Add(time.Second), Timeline: model.SchedulerTimeline{Window: time.Second, Events: []model.SchedEvent{
		{At: 0, Kind: model.SchedEventGoroutine, Goroutine: 7, Place: blocked},
		{At: 400 * ms, Kind: model.SchedEventGoroutine, Goroutine: 7, Place: model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: model.UnknownWaitReason}},
	}}}

	tests := []struct {
		name      string
		chunks    []model.TraceChunk
		at        time.Time
		wantSince time.Time
		wantCause domain.PrimaryCause
		wantOK    bool
	}{
		{name: "success: the state began in an earlier chunk", chunks: []model.TraceChunk{first, second}, at: base.Add(1500 * ms), wantSince: base.Add(300 * ms), wantCause: domain.CauseBlocked, wantOK: true},
		{name: "success: the state at the time, not a later one", chunks: []model.TraceChunk{first, second}, at: base.Add(200 * ms), wantSince: base.Add(100 * ms), wantCause: domain.CauseRunning, wantOK: true},
		{name: "error: the state began before the recording", chunks: []model.TraceChunk{second}, at: base.Add(1500 * ms), wantCause: domain.CauseBlocked},
		{name: "error: the goroutine does not appear by the time", chunks: []model.TraceChunk{first}, at: base.Add(50 * ms)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			since, cause, ok := domain.StateSince(tt.chunks, 7, tt.at)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantCause, cause)
			if tt.wantOK {
				assert.Equal(t, tt.wantSince, since)
			}
		})
	}
}
