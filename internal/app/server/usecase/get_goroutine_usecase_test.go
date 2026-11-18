package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/adapter/kvs"
	"github.com/kakkky/gorviz/internal/app/server/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

func TestGetGoroutineUsecase_Exec(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	gEv := func(at time.Duration, place model.GoroutinePlace, fn, loc string) model.SchedEvent {
		return model.SchedEvent{At: at, Kind: model.SchedEventGoroutine, Goroutine: 5, Place: place, Function: fn, Location: loc}
	}
	running := model.GoroutinePlace{Kind: model.PlaceRunning, HasProc: true}
	network := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "network"}

	traces := &fakeTraces{}
	// 2 つの区切りの間に 100ms のすき間がある (録画の HTTP の往復)
	traces.Append(model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{
		Window:  time.Second,
		Entries: map[model.GoroutineID]string{5: "main.handle"},
		Events:  []model.SchedEvent{gEv(0, running, "", ""), gEv(800*ms, network, "net.(*conn).Read", "/app/db.go:42")},
	}})
	traces.Append(model.TraceChunk{StartedAt: base.Add(1100 * ms), Timeline: model.SchedulerTimeline{
		Window: time.Second,
		Events: []model.SchedEvent{gEv(0, network, "", ""), gEv(400*ms, running, "", "")},
	}})
	uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), traces, recording.NewLogStore(100), nil, nil, nil, nil)

	out, err := uc.NewGetGoroutineUsecase().Exec(context.Background(), usecase.GetGoroutineUsecaseInput{
		Goroutine: 5, From: base.Add(500 * ms), To: base.Add(1600 * ms), At: base.Add(900 * ms), ReplayHalf: 50 * ms,
	})
	require.NoError(t, err)

	assert.Equal(t, map[domain.PrimaryCause]time.Duration{domain.CauseRunning: 300*ms + 100*ms, domain.CauseIOWait: 200*ms + 400*ms}, out.Breakdown.Durations, "time is summed across chunks")
	assert.Equal(t, 100*ms, out.Breakdown.Unrecorded, "the gap between chunks is unrecorded")
	assert.Equal(t, domain.CauseIOWait, out.Primary)
	assert.Equal(t, domain.CertaintyFact, out.Certainty)
	require.Len(t, out.Causes, len(domain.PrimaryCauses), "every cause is listed in the display order")
	for i, c := range out.Causes {
		assert.Equal(t, domain.PrimaryCauses[i], c.Cause)
		assert.Equal(t, out.Breakdown.Durations[c.Cause], c.Duration)
		assert.Equal(t, c.Cause == domain.CauseIOWait, c.Primary, "only the recorded primary cause is marked")
	}
	assert.Equal(t, "net.(*conn).Read", out.Function)
	assert.Equal(t, "/app/db.go:42", out.Location)
	assert.Equal(t, "network", out.WaitReason)
	assert.Equal(t, "main.handle", out.Entry)
	assert.True(t, out.HasReplay)
	assert.Equal(t, 50*ms, out.ReplayFocus)
}

func TestGetGoroutineUsecase_Exec_StateSinceAcrossAGap(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	running := model.GoroutinePlace{Kind: model.PlaceRunning, Proc: 0, HasProc: true}
	blocked := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "chan send"}
	traces := &fakeTraces{}
	// trace を順に取る agent では、区切りのあいだが少し空く
	traces.Append(model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{Window: 2 * time.Second, Events: []model.SchedEvent{
		{At: 100 * ms, Kind: model.SchedEventGoroutine, Goroutine: 7, Place: running},
		{At: 300 * ms, Kind: model.SchedEventGoroutine, Goroutine: 7, Place: blocked},
	}}})
	traces.Append(model.TraceChunk{StartedAt: base.Add(2050 * ms), Timeline: model.SchedulerTimeline{Window: 2 * time.Second, Events: []model.SchedEvent{
		{At: 0, Kind: model.SchedEventGoroutine, Goroutine: 7, Place: blocked},
	}}})
	uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), traces, recording.NewLogStore(10), nil, nil, nil, nil)
	at := base.Add(3 * time.Second)

	out, err := uc.NewGetGoroutineUsecase().Exec(context.Background(), usecase.GetGoroutineUsecaseInput{Goroutine: 7, At: at, From: at.Add(-250 * ms), To: at.Add(250 * ms), ReplayHalf: 250 * ms})
	require.NoError(t, err)

	require.True(t, out.HasStateSince, "the walk back jumps over the gap between chunks")
	assert.Equal(t, base.Add(300*ms), out.StateSince)
}
