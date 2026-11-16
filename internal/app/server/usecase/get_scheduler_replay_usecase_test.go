package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/adapter/kvs"
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
			uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), traces, nil, nil, nil, nil)

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
		uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), &fakeTraces{}, nil, nil, nil, nil)

		_, err := uc.NewGetSchedulerReplayUsecase().Exec(context.Background(), usecase.GetSchedulerReplayUsecaseInput{Length: time.Second})
		assert.ErrorIs(t, err, model.ErrNotFound)
	})
}
