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
