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

func TestLocateLogUsecase_Exec(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	newUsecase := func() (*usecase.UseCase, *fakeTraces, *recording.LogStore) {
		traces, logs := &fakeTraces{}, recording.NewLogStore(100)
		return usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), traces, logs, nil, nil, nil, nil), traces, logs
	}
	chunk := model.TraceChunk{
		StartedAt: base,
		Timeline:  model.SchedulerTimeline{Window: time.Second},
		LogMarks:  []model.TraceLogMark{{Seq: 9, Goroutine: 5, At: 400 * ms}},
	}

	t.Run("success: log written through the slog handler is located by its trace mark", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		traces.Append(chunk)
		// ログに書かれた時刻はずれていても、印の時刻と G を使う
		line := logs.Append(model.LogLine{At: base.Add(450 * ms), Message: "seq", TraceSeq: 9, HasTraceSeq: true})

		out, err := uc.NewLocateLogUsecase().Exec(context.Background(), usecase.LocateLogUsecaseInput{LogID: line.ID})
		require.NoError(t, err)

		assert.Equal(t, usecase.LogMark{Line: line, At: base.Add(400 * ms), Goroutine: 5, Exact: true}, out.Mark)
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

		out, err := uc.NewLocateLogUsecase().Exec(context.Background(), usecase.LocateLogUsecaseInput{LogID: line.ID})
		require.NoError(t, err)

		assert.Equal(t, usecase.LogMark{Line: line, At: base.Add(100 * ms), Goroutine: 8, Inferred: true}, out.Mark)
		assert.True(t, out.Mark.HasGoroutine())
	})

	t.Run("success: plain log without a single running goroutine has no writer", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		traces.Append(chunk)
		line := logs.Append(model.LogLine{At: base.Add(100 * ms), Message: "plain"})

		out, err := uc.NewLocateLogUsecase().Exec(context.Background(), usecase.LocateLogUsecaseInput{LogID: line.ID})
		require.NoError(t, err)

		assert.False(t, out.Mark.HasGoroutine())
	})

	t.Run("error: log outside the recorded traces", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		traces.Append(chunk)
		line := logs.Append(model.LogLine{At: base.Add(5 * time.Second), Message: "late"})

		_, err := uc.NewLocateLogUsecase().Exec(context.Background(), usecase.LocateLogUsecaseInput{LogID: line.ID})
		assert.ErrorIs(t, err, model.ErrNotFound)
	})
}
