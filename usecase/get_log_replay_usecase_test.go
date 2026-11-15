package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/adapter/kvs"
	"github.com/kakkky/gorviz/adapter/logparse"
	"github.com/kakkky/gorviz/adapter/recording"
	"github.com/kakkky/gorviz/adapter/schedtrace"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/usecase"
)

func TestGetLogReplayUsecase_Exec(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{Retention: time.Hour, MaxRecordedEvents: 1000, MaxLogLines: 100}

	newUsecase := func() (*usecase.UseCase, *recording.TraceStore, *recording.LogStore) {
		traces, logs := recording.NewTraceStore(cfg), recording.NewLogStore(cfg)
		return usecase.NewUseCase(cfg, nil, kvs.NewInMemoryKVS(), schedtrace.NewStore(), traces, logs, logparse.NewParser()), traces, logs
	}
	chunk := domain.TraceChunk{
		StartedAt: base,
		Timeline: domain.SchedulerTimeline{
			Window: time.Second,
			Events: []domain.SchedEvent{
				{At: 100 * ms, Kind: domain.SchedEventGoroutine, Goroutine: 5, Place: domain.GoroutinePlace{Kind: domain.PlaceGlobalQueue}},
				{At: 600 * ms, Kind: domain.SchedEventGoroutine, Goroutine: 5, Place: domain.GoroutinePlace{Kind: domain.PlaceDead}},
			},
		},
		LogMarks: []domain.TraceLogMark{{Seq: 9, Goroutine: 5, At: 400 * ms}},
	}

	t.Run("success: log written through the slog handler is located by its trace mark", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		traces.Append(chunk)
		// ログに書かれた時刻はずれていても、印の時刻と G を使う
		line := logs.Append(domain.LogLine{At: base.Add(450 * ms), Message: "seq", TraceSeq: 9, HasTraceSeq: true})
		other := logs.Append(domain.LogLine{At: base.Add(500 * ms), Message: "nearby"})

		out, err := uc.NewGetLogReplayUsecase().Exec(context.Background(), usecase.GetLogReplayUsecaseInput{LogID: line.ID, Before: 200 * ms, After: 200 * ms})
		require.NoError(t, err)

		assert.Equal(t, base.Add(200*ms), out.WindowStartedAt)
		assert.Equal(t, 400*ms, out.Timeline.Window)
		assert.Equal(t, usecase.GetLogReplayUsecaseLogMark{Line: line, At: 200 * ms, Goroutine: 5, Exact: true}, out.Focus)
		assert.Equal(t, []usecase.GetLogReplayUsecaseLogMark{
			{Line: line, At: 200 * ms, Goroutine: 5, Exact: true},
			{Line: other, At: 300 * ms},
		}, out.WindowLogs)
		assert.Equal(t, domain.PlaceGlobalQueue, out.Timeline.Events[0].Place.Kind, "state before the window is carried in")
	})

	t.Run("success: plain log is located by its timestamp", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		traces.Append(chunk)
		line := logs.Append(domain.LogLine{At: base.Add(100 * ms), Message: "plain"})

		out, err := uc.NewGetLogReplayUsecase().Exec(context.Background(), usecase.GetLogReplayUsecaseInput{LogID: line.ID, Before: 200 * ms, After: 200 * ms})
		require.NoError(t, err)

		assert.Equal(t, base, out.WindowStartedAt, "window is clamped at the start of the chunk")
		assert.Equal(t, usecase.GetLogReplayUsecaseLogMark{Line: line, At: 100 * ms}, out.Focus)
	})

	t.Run("error: log outside the recorded traces", func(t *testing.T) {
		t.Parallel()
		uc, traces, logs := newUsecase()
		traces.Append(chunk)
		line := logs.Append(domain.LogLine{At: base.Add(5 * time.Second), Message: "late"})

		_, err := uc.NewGetLogReplayUsecase().Exec(context.Background(), usecase.GetLogReplayUsecaseInput{LogID: line.ID, Before: 200 * ms, After: 200 * ms})
		assert.ErrorIs(t, err, domain.ErrNotFound)
	})
}
