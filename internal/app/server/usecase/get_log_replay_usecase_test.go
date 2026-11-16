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
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

func TestGetLogReplayUsecase_Exec(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	newUsecase := func() (*usecase.UseCase, *fakeTraces, *recording.LogStore) {
		traces, logs := &fakeTraces{}, recording.NewLogStore(100)
		return usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), traces, logs, nil, nil, nil), traces, logs
	}
	chunk := model.TraceChunk{
		StartedAt: base,
		Timeline: model.SchedulerTimeline{
			Window: time.Second,
			Events: []model.SchedEvent{
				{At: 100 * ms, Kind: model.SchedEventGoroutine, Goroutine: 5, Place: model.GoroutinePlace{Kind: model.PlaceGlobalQueue}},
				{At: 600 * ms, Kind: model.SchedEventGoroutine, Goroutine: 5, Place: model.GoroutinePlace{Kind: model.PlaceDead}},
			},
		},
		LogMarks: []model.TraceLogMark{{Seq: 9, Goroutine: 5, At: 400 * ms}},
	}

	t.Run("success: log written through the slog handler is located by its trace mark", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		traces.Append(chunk)
		// ログに書かれた時刻はずれていても、印の時刻と G を使う
		line := logs.Append(model.LogLine{At: base.Add(450 * ms), Message: "seq", TraceSeq: 9, HasTraceSeq: true})
		other := logs.Append(model.LogLine{At: base.Add(500 * ms), Message: "nearby"})

		out, err := uc.NewGetLogReplayUsecase().Exec(context.Background(), usecase.GetLogReplayUsecaseInput{LogID: line.ID, Before: 200 * ms, After: 200 * ms})
		require.NoError(t, err)

		assert.Equal(t, base.Add(200*ms), out.WindowStartedAt)
		assert.Equal(t, 400*ms, out.Timeline.Window)
		assert.Equal(t, usecase.GetLogReplayUsecaseLogMark{Line: line, At: 200 * ms, Goroutine: 5, Exact: true}, out.Focus)
		assert.Equal(t, []usecase.GetLogReplayUsecaseLogMark{
			{Line: line, At: 200 * ms, Goroutine: 5, Exact: true},
			{Line: other, At: 300 * ms},
		}, out.WindowLogs)
		assert.Equal(t, model.PlaceGlobalQueue, out.Timeline.Events[0].Place.Kind, "state before the window is carried in")
	})

	t.Run("success: plain log is located by its timestamp", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		traces.Append(chunk)
		line := logs.Append(model.LogLine{At: base.Add(100 * ms), Message: "plain"})

		out, err := uc.NewGetLogReplayUsecase().Exec(context.Background(), usecase.GetLogReplayUsecaseInput{LogID: line.ID, Before: 200 * ms, After: 200 * ms})
		require.NoError(t, err)

		assert.Equal(t, base, out.WindowStartedAt, "window is clamped at the start of the chunk")
		assert.Equal(t, usecase.GetLogReplayUsecaseLogMark{Line: line, At: 100 * ms}, out.Focus)
	})

	t.Run("success: plain log is attributed to the only running goroutine as an inference", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		running := chunk
		running.Timeline.Procs = []model.ProcLane{{Proc: 0}}
		running.Timeline.Events = []model.SchedEvent{
			{At: 0, Kind: model.SchedEventProc, Proc: 0, ProcStatus: model.ProcRunning},
			{At: 50 * ms, Kind: model.SchedEventGoroutine, Goroutine: 8, Place: model.GoroutinePlace{Kind: model.PlaceRunning, Proc: 0, HasProc: true}},
		}
		traces.Append(running)
		line := logs.Append(model.LogLine{At: base.Add(100 * ms), Message: "plain"})

		out, err := uc.NewGetLogReplayUsecase().Exec(context.Background(), usecase.GetLogReplayUsecaseInput{LogID: line.ID, Before: 200 * ms, After: 200 * ms})
		require.NoError(t, err)

		assert.Equal(t, usecase.GetLogReplayUsecaseLogMark{Line: line, At: 100 * ms, Goroutine: 8, Inferred: true}, out.Focus)
	})

	t.Run("error: log outside the recorded traces", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		traces.Append(chunk)
		line := logs.Append(model.LogLine{At: base.Add(5 * time.Second), Message: "late"})

		_, err := uc.NewGetLogReplayUsecase().Exec(context.Background(), usecase.GetLogReplayUsecaseInput{LogID: line.ID, Before: 200 * ms, After: 200 * ms})
		assert.ErrorIs(t, err, model.ErrNotFound)
	})
}
