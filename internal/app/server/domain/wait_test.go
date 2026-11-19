package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestStretches(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	gEv := func(at time.Duration, place model.GoroutinePlace, fn string) model.SchedEvent {
		return model.SchedEvent{At: at, Kind: model.SchedEventGoroutine, Goroutine: 1, Place: place, Function: fn}
	}
	lock := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "sync"}
	running := model.GoroutinePlace{Kind: model.PlaceRunning, HasProc: true}
	woken := gEv(30*ms, model.GoroutinePlace{Kind: model.PlaceLocalQueue}, "")
	woken.WokenBy = 2
	tl := model.SchedulerTimeline{Window: 100 * ms, Events: []model.SchedEvent{
		gEv(10*ms, lock, "sync.(*Mutex).Lock"),
		gEv(20*ms, model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: model.UnknownWaitReason}, ""),
		woken,
		gEv(31*ms, running, ""),
		gEv(60*ms, lock, "sync.(*Mutex).Lock"),
	}}

	got := domain.Stretches(tl, 1, domain.CauseBlocked, 0, tl.Window)

	require.Len(t, got, 2, "a rewrite of the same state does not split the stretch")
	assert.Equal(t, 20*ms, got[0].Duration())
	assert.Equal(t, "sync.(*Mutex).Lock", got[0].Enter.Function)
	assert.True(t, got[0].HasLeave)
	assert.Equal(t, model.GoroutineID(2), got[0].Leave.WokenBy)
	assert.False(t, got[1].HasLeave, "still waiting at the end of the window")
	assert.Equal(t, 100*ms, got[1].End)
}

func TestCauseOfWaitSite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		site model.WaitSite
		want domain.PrimaryCause
	}{
		{name: "success: the wait reason decides", site: model.WaitSite{Kind: model.PlaceWaiting, Reason: "chan receive", Function: "net.(*netFD).Read"}, want: domain.CauseBlocked},
		{name: "success: a system call is I/O", site: model.WaitSite{Kind: model.PlaceSyscall}, want: domain.CauseIOWait},
		{name: "success: a cgo call", site: model.WaitSite{Kind: model.PlaceSyscall, Cgo: true}, want: domain.CauseCGO},
		{name: "success: an unknown reason is guessed from the function", site: model.WaitSite{Kind: model.PlaceWaiting, Reason: model.UnknownWaitReason, Function: "net.(*netFD).accept"}, want: domain.CauseIOWait},
		{name: "success: an unknown reason in user code is blocked", site: model.WaitSite{Kind: model.PlaceWaiting, Reason: model.UnknownWaitReason, Function: "main.worker"}, want: domain.CauseBlocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.CauseOfWaitSite(tt.site))
		})
	}
}

func TestMetricPoint_ObserveTrace(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	stats := model.TraceStats{
		Window: time.Second, Procs: 4, GCMaxShare: 0.25,
		ProcRunning: 2 * time.Second, ProcGC: 400 * ms,
		Waits: []model.WaitSite{
			{Kind: model.PlaceWaiting, Reason: "sync", Entry: "main.handle", Function: "sync.(*Mutex).Lock", Location: "/app/store.go:38", Count: 3, Total: 1500 * ms, Max: 900 * ms},
		},
	}
	stats.SchedLatency.Observe(5 * ms)
	var p domain.MetricPoint
	p.ObserveTrace(stats)
	p.ObserveTrace(stats)

	assert.InDelta(t, 0.5, p.ProcRunning.Max, 1e-9, "running time over the time of all P")
	assert.InDelta(t, 0.1, p.ProcGC.Max, 1e-9)
	assert.InDelta(t, 2.0, p.TracedSeconds, 1e-9)
	assert.Equal(t, uint64(2), p.SchedLatency.N())
	key := domain.WaitSiteKey{Cause: domain.CauseBlocked, Entry: "main.handle", Function: "sync.(*Mutex).Lock", Location: "/app/store.go:38"}
	assert.Equal(t, domain.WaitTotal{Seconds: 3, Count: 6, MaxSeconds: 0.9, Reason: "sync"}, p.Waits[key])

	b, err := json.Marshal(p)
	require.NoError(t, err)
	var back domain.MetricPoint
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, p.Waits, back.Waits, "the wait sites survive the round trip through the stored JSON")
}
