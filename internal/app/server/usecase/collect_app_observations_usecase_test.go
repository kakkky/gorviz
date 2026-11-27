package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kakkky/gorviz/internal/app/server/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
	"github.com/kakkky/gorviz/internal/app/server/testhelper/mock"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

func TestCollectAppObservationsUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	wantErr := errors.New("boom")

	tests := []struct {
		name      string
		state     domain.AppState
		collected domain.Collected
		waiting   int
		mock      func(a *mock.MockAgentInstructor, c *mock.MockClock)
		want      domain.Collected
		wantErr   error
	}{
		{
			name:    "asks for both the first time",
			state:   domain.AppRunning,
			waiting: 3,
			mock: func(a *mock.MockAgentInstructor, c *mock.MockClock) {
				a.EXPECT().Connected().Return(true)
				c.EXPECT().Now().Return(now)
				a.EXPECT().RequestThreadCount(gomock.Any()).Return(nil)
				a.EXPECT().RequestGoroutineLeaks(gomock.Any()).Return(nil)
			},
			want: domain.Collected{Threads: now, Leaks: now, LeakWaiting: 3},
		},
		{
			name:      "asks for the leak profile when the waiting goroutines grew",
			state:     domain.AppRunning,
			collected: domain.Collected{Threads: now.Add(-time.Second), Leaks: now.Add(-20 * time.Second), LeakWaiting: 3},
			waiting:   5,
			mock: func(a *mock.MockAgentInstructor, c *mock.MockClock) {
				a.EXPECT().Connected().Return(true)
				c.EXPECT().Now().Return(now)
				a.EXPECT().RequestGoroutineLeaks(gomock.Any()).Return(nil)
			},
			want: domain.Collected{Threads: now.Add(-time.Second), Leaks: now, LeakWaiting: 5},
		},
		{
			name:      "skips the leak profile while the waiting goroutines did not grow",
			state:     domain.AppRunning,
			collected: domain.Collected{Threads: now.Add(-3 * time.Second), Leaks: now.Add(-30 * time.Second), LeakWaiting: 5},
			waiting:   5,
			mock: func(a *mock.MockAgentInstructor, c *mock.MockClock) {
				a.EXPECT().Connected().Return(true)
				c.EXPECT().Now().Return(now)
				a.EXPECT().RequestThreadCount(gomock.Any()).Return(nil)
			},
			want: domain.Collected{Threads: now, Leaks: now.Add(-30 * time.Second), LeakWaiting: 5},
		},
		{
			name:      "skips the leak profile before the shortest gap even when the waiting goroutines grew",
			state:     domain.AppRunning,
			collected: domain.Collected{Threads: now.Add(-time.Second), Leaks: now.Add(-5 * time.Second), LeakWaiting: 3},
			waiting:   9,
			mock: func(a *mock.MockAgentInstructor, c *mock.MockClock) {
				a.EXPECT().Connected().Return(true)
				c.EXPECT().Now().Return(now)
			},
			want: domain.Collected{Threads: now.Add(-time.Second), Leaks: now.Add(-5 * time.Second), LeakWaiting: 3},
		},
		{
			name:      "asks for the leak profile after the longest gap even when the waiting goroutines did not grow",
			state:     domain.AppRunning,
			collected: domain.Collected{Threads: now.Add(-time.Second), Leaks: now.Add(-time.Minute), LeakWaiting: 5},
			waiting:   4,
			mock: func(a *mock.MockAgentInstructor, c *mock.MockClock) {
				a.EXPECT().Connected().Return(true)
				c.EXPECT().Now().Return(now)
				a.EXPECT().RequestGoroutineLeaks(gomock.Any()).Return(nil)
			},
			want: domain.Collected{Threads: now.Add(-time.Second), Leaks: now, LeakWaiting: 4},
		},
		{
			name:  "asks nothing when the app is not running",
			state: domain.AppExited,
			mock:  func(*mock.MockAgentInstructor, *mock.MockClock) {},
		},
		{
			name:  "returns the error of the agent without marking it collected",
			state: domain.AppRunning,
			mock: func(a *mock.MockAgentInstructor, c *mock.MockClock) {
				a.EXPECT().Connected().Return(true)
				c.EXPECT().Now().Return(now)
				a.EXPECT().RequestThreadCount(gomock.Any()).Return(wantErr)
			},
			wantErr: wantErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			agent := mock.NewMockAgentInstructor(ctrl)
			clock := mock.NewMockClock(ctrl)
			tt.mock(agent, clock)
			store := kvs.NewInMemoryKVS()
			sessions := accessor.NewSessionAccessor(store)
			observations := accessor.NewObservationAccessor(store)
			require.NoError(t, observations.Put(domain.Observation{Second: 2, Stats: domain.Bucket{Waiting: tt.waiting}}))
			sessions.UpdateSession(func(s *domain.Session) {
				s.App.State, s.Collected, s.Observed.Until = tt.state, tt.collected, 3*domain.BucketSize
			})

			uc := usecase.NewUseCase(sessions, observations, nil, nil, agent, nil, nil, nil, nil, clock).NewCollectAppObservationsUsecase()
			out, err := uc.Exec(ctx, usecase.CollectAppObservationsUsecaseInput{})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.CollectAppObservationsUsecaseOutput{}, out)
				assert.Equal(t, tt.collected, sessions.Session().Collected)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, sessions.Session().Collected)
		})
	}
}
