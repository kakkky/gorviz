package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/adapter/kvs"
	"github.com/kakkky/gorviz/internal/app/server/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

func TestGetSchedulerReplayUsecase_Exec(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		at          time.Time
		length      time.Duration
		wantStarted time.Time
		wantWindow  time.Duration
		wantErr     bool
	}{
		{name: "success: the end of the latest chunk", length: 500 * ms, wantStarted: base.Add(3500 * ms), wantWindow: 500 * ms},
		{name: "success: longer than the chunk means the whole chunk", length: 5 * time.Second, wantStarted: base.Add(2 * time.Second), wantWindow: 2 * time.Second},
		{name: "success: centered on the given time", at: base.Add(time.Second), length: 500 * ms, wantStarted: base.Add(750 * ms), wantWindow: 500 * ms},
		{name: "success: shifted forward at the start of the chunk", at: base.Add(100 * ms), length: 500 * ms, wantStarted: base, wantWindow: 500 * ms},
		{name: "success: shifted back at the end of the chunk", at: base.Add(1900 * ms), length: 500 * ms, wantStarted: base.Add(1500 * ms), wantWindow: 500 * ms},
		{name: "error: not in the recording", at: base.Add(-time.Second), length: 500 * ms, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			traces := &fakeTraces{}
			traces.Append(model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{Window: 2 * time.Second}})
			traces.Append(model.TraceChunk{StartedAt: base.Add(2 * time.Second), Timeline: model.SchedulerTimeline{Window: 2 * time.Second}})
			uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), traces, recording.NewLogStore(10), nil, nil, nil, nil)

			out, err := uc.NewGetSchedulerReplayUsecase().Exec(context.Background(), usecase.GetSchedulerReplayUsecaseInput{At: tt.at, Length: tt.length})
			if tt.wantErr {
				assert.ErrorIs(t, err, model.ErrNotFound)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStarted, out.StartedAt)
			assert.Equal(t, tt.wantWindow, out.Timeline.Window)
		})
	}

	t.Run("error: nothing recorded", func(t *testing.T) {
		t.Parallel()
		uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), &fakeTraces{}, recording.NewLogStore(10), nil, nil, nil, nil)

		_, err := uc.NewGetSchedulerReplayUsecase().Exec(context.Background(), usecase.GetSchedulerReplayUsecaseInput{Length: time.Second})
		assert.ErrorIs(t, err, model.ErrNotFound)
	})
}

func TestGetSchedulerReplayUsecase_Exec_Adjacent(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		after       time.Time
		before      time.Time
		wantStarted time.Time
		wantWindow  time.Duration
		wantErr     bool
	}{
		{name: "success: the next window starts where the last one ended", after: base.Add(500 * ms), wantStarted: base.Add(500 * ms), wantWindow: 500 * ms},
		{name: "success: cut short at the end of the chunk", after: base.Add(1800 * ms), wantStarted: base.Add(1800 * ms), wantWindow: 200 * ms},
		{name: "success: jumps over the gap to the next chunk", after: base.Add(2 * time.Second), wantStarted: base.Add(2050 * ms), wantWindow: 500 * ms},
		{name: "success: the previous window ends where the current one started", before: base.Add(1 * time.Second), wantStarted: base.Add(500 * ms), wantWindow: 500 * ms},
		{name: "success: jumps back over the gap to the previous chunk", before: base.Add(2050 * ms), wantStarted: base.Add(1500 * ms), wantWindow: 500 * ms},
		{name: "error: nothing after the latest chunk", after: base.Add(4050 * ms), wantErr: true},
		{name: "error: nothing before the first chunk", before: base, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			traces := &fakeTraces{}
			// trace を順に取る agent では、区切りのあいだが少し空く
			traces.Append(model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{Window: 2 * time.Second}})
			traces.Append(model.TraceChunk{StartedAt: base.Add(2050 * ms), Timeline: model.SchedulerTimeline{Window: 2 * time.Second}})
			uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), traces, recording.NewLogStore(10), nil, nil, nil, nil)

			out, err := uc.NewGetSchedulerReplayUsecase().Exec(context.Background(), usecase.GetSchedulerReplayUsecaseInput{After: tt.after, Before: tt.before, Length: 500 * ms})
			if tt.wantErr {
				assert.ErrorIs(t, err, model.ErrNotFound)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStarted, out.StartedAt)
			assert.Equal(t, tt.wantWindow, out.Timeline.Window)
		})
	}
}
