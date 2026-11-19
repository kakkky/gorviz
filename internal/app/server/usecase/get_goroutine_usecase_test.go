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
	assert.Equal(t, base.Add(500*ms), out.ReplayStartedAt, "the replay covers the diagnosed interval, not only around At")
	assert.Equal(t, 400*ms, out.ReplayFocus)
	assert.Equal(t, 1100*ms, out.Replay.Window, "the two chunks are joined into one replay")
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

func TestGetGoroutineUsecase_Exec_WokenBy(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	gEv := func(at time.Duration, g model.GoroutineID, place model.GoroutinePlace, fn, loc string) model.SchedEvent {
		return model.SchedEvent{At: at, Kind: model.SchedEventGoroutine, Goroutine: g, Place: place, Function: fn, Location: loc}
	}
	running := model.GoroutinePlace{Kind: model.PlaceRunning, HasProc: true}
	lock := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "sync"}
	sleep := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "sleep"}
	woken := gEv(300*ms, 5, model.GoroutinePlace{Kind: model.PlaceLocalQueue}, "", "")
	woken.WokenBy, woken.WokenByFunction, woken.WokenByLocation = 9, "sync.(*Mutex).Unlock", "/app/backup.go:80"

	traces := &fakeTraces{}
	// G5 はロックで待ち始め、区切りをまたいで、ロックを持った G9 (スリープ中) が Unlock したところで起こされる
	traces.Append(model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{
		Window:  time.Second,
		Entries: map[model.GoroutineID]string{9: "main.(*Backup).Run"},
		Events: []model.SchedEvent{
			gEv(0, 9, sleep, "time.Sleep", "/app/backup.go:78"),
			gEv(600*ms, 5, lock, "sync.(*Mutex).Lock", "/app/store.go:38"),
		},
	}})
	traces.Append(model.TraceChunk{StartedAt: base.Add(time.Second), Timeline: model.SchedulerTimeline{
		Window: time.Second,
		Events: []model.SchedEvent{
			gEv(0, 9, sleep, "", ""),
			gEv(0, 5, lock, "", ""),
			gEv(290*ms, 9, running, "", ""),
			woken,
			gEv(310*ms, 5, running, "", ""),
		},
	}})
	uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), traces, recording.NewLogStore(100), nil, nil, nil, nil)

	out, err := uc.NewGetGoroutineUsecase().Exec(context.Background(), usecase.GetGoroutineUsecaseInput{
		Goroutine: 5, From: base.Add(500 * ms), To: base.Add(1500 * ms), At: base.Add(1400 * ms), ReplayHalf: 50 * ms,
	})
	require.NoError(t, err)

	require.True(t, out.HasLongest)
	l := out.Longest
	assert.Equal(t, base.Add(600*ms), l.From, "the wait that runs into the next chunk is one stretch")
	assert.Equal(t, base.Add(1300*ms), l.To)
	assert.Equal(t, "/app/store.go:38", l.Location)
	assert.True(t, l.HasWake)
	assert.Equal(t, model.GoroutineID(9), l.Waker)
	assert.Equal(t, "/app/backup.go:80", l.WakerLocation)
	assert.Equal(t, "main.(*Backup).Run", l.WakerEntry)
	assert.Equal(t, domain.CauseSleep, l.WakerPrimary, "what the goroutine holding the lock was doing meanwhile")
	assert.Equal(t, "/app/backup.go:78", l.WakerBusyLocation)
}
