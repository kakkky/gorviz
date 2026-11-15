package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/adapter/kvs"
	"github.com/kakkky/gorviz/adapter/recording"
	"github.com/kakkky/gorviz/adapter/schedtrace"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/usecase"
)

func TestGetGoroutineUsecase_Exec(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{TraceBufferBytes: 1 << 20, MaxLogLines: 100}
	gEv := func(at time.Duration, place domain.GoroutinePlace, fn, loc string) domain.SchedEvent {
		return domain.SchedEvent{At: at, Kind: domain.SchedEventGoroutine, Goroutine: 5, Place: place, Function: fn, Location: loc}
	}
	running := domain.GoroutinePlace{Kind: domain.PlaceRunning, HasProc: true}
	network := domain.GoroutinePlace{Kind: domain.PlaceWaiting, Reason: "network"}

	traces := recording.NewTraceStore(cfg)
	// 2 つの区切りの間に 100ms のすき間がある (録画の HTTP の往復)
	traces.Append(domain.TraceChunk{StartedAt: base, Timeline: domain.SchedulerTimeline{
		Window:  time.Second,
		Entries: map[domain.GoroutineID]string{5: "main.handle"},
		Events:  []domain.SchedEvent{gEv(0, running, "", ""), gEv(800*ms, network, "net.(*conn).Read", "/app/db.go:42")},
	}})
	traces.Append(domain.TraceChunk{StartedAt: base.Add(1100 * ms), Timeline: domain.SchedulerTimeline{
		Window: time.Second,
		Events: []domain.SchedEvent{gEv(0, network, "", ""), gEv(400*ms, running, "", "")},
	}})
	uc := usecase.NewServerUseCase(cfg, nil, nil, kvs.NewInMemoryKVS(), schedtrace.NewStore(), traces, recording.NewLogStore(cfg), nil)

	out, err := uc.NewGetGoroutineUsecase().Exec(context.Background(), usecase.GetGoroutineUsecaseInput{
		Goroutine: 5, From: base.Add(500 * ms), To: base.Add(1600 * ms), At: base.Add(900 * ms), ReplayHalf: 50 * ms,
	})
	require.NoError(t, err)

	assert.Equal(t, map[domain.PrimaryCause]time.Duration{domain.CauseRunning: 300*ms + 100*ms, domain.CauseIOWait: 200*ms + 400*ms}, out.Breakdown.Durations, "time is summed across chunks")
	assert.Equal(t, 100*ms, out.Breakdown.Unrecorded, "the gap between chunks is unrecorded")
	assert.Equal(t, domain.CauseIOWait, out.Primary)
	assert.Equal(t, domain.CertaintyFact, out.Certainty)
	assert.Equal(t, "net.(*conn).Read", out.Function)
	assert.Equal(t, "/app/db.go:42", out.Location)
	assert.Equal(t, "network", out.WaitReason)
	assert.Equal(t, "main.handle", out.Entry)
	assert.True(t, out.HasReplay)
	assert.Equal(t, 50*ms, out.ReplayFocus)
}
