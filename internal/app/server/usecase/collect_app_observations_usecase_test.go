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

const leakProfile = `goroutineleak profile: total 2
2 @ 0x1 0x2
#	0x1	main.leak.func1+0x10	/app/main.go:23
#	0x2	runtime.goexit+0x4	/go/src/runtime/asm_arm64.s:1
`

func TestCollectAppObservationsUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	clockSync := domain.ClockSync{At: 2 * time.Second, Wall: now.Add(-time.Second)}
	wantErr := errors.New("boom")

	tests := []struct {
		name         string
		state        domain.AppState
		shuttingDown bool
		collected    domain.Collected
		waiting      int
		mock         func(p *mock.MockAppProfiler, c *mock.MockClock)
		want         usecase.CollectAppObservationsUsecaseOutput
		wantPending  []domain.PendingLeakCheck
		wantErr      error
	}{
		{
			name:    "takes the leak profile the first time and puts it on the trace",
			state:   domain.AppRunning,
			waiting: 3,
			mock: func(p *mock.MockAppProfiler, c *mock.MockClock) {
				c.EXPECT().Now().Return(now)
				p.EXPECT().FetchGoroutineLeakProfile(gomock.Any()).Return([]byte(leakProfile), nil)
			},
			want: usecase.CollectAppObservationsUsecaseOutput{TookLeaks: true},
			wantPending: []domain.PendingLeakCheck{{At: 3 * time.Second, Stacks: []domain.LeakProfileStack{{Count: 2, Frames: []domain.Frame{
				{Function: "main.leak.func1", File: "/app/main.go", Line: 23}, {Function: "runtime.goexit", File: "/go/src/runtime/asm_arm64.s", Line: 1},
			}}}}},
		},
		{
			name:      "skips the leak profile while the waiting goroutines did not grow",
			state:     domain.AppRunning,
			collected: domain.Collected{Leaks: now.Add(-30 * time.Second), LeakWaiting: 5},
			waiting:   5,
			mock: func(_ *mock.MockAppProfiler, c *mock.MockClock) {
				c.EXPECT().Now().Return(now)
			},
		},
		{
			name:         "takes the leak profile every second during the shutdown",
			state:        domain.AppRunning,
			shuttingDown: true,
			collected:    domain.Collected{Leaks: now.Add(-time.Second), LeakWaiting: 5},
			waiting:      5,
			mock: func(p *mock.MockAppProfiler, c *mock.MockClock) {
				c.EXPECT().Now().Return(now)
				p.EXPECT().FetchGoroutineLeakProfile(gomock.Any()).Return([]byte("goroutineleak profile: total 0\n"), nil)
			},
			want:        usecase.CollectAppObservationsUsecaseOutput{TookLeaks: true},
			wantPending: []domain.PendingLeakCheck{{At: 3 * time.Second}},
		},
		{
			name:  "says the app is gone once it exited",
			state: domain.AppExited,
			mock:  func(*mock.MockAppProfiler, *mock.MockClock) {},
			want:  usecase.CollectAppObservationsUsecaseOutput{AppGone: true},
		},
		{
			name:  "returns the error of the pprof without marking it collected",
			state: domain.AppRunning,
			mock: func(p *mock.MockAppProfiler, c *mock.MockClock) {
				c.EXPECT().Now().Return(now)
				p.EXPECT().FetchGoroutineLeakProfile(gomock.Any()).Return(nil, wantErr)
			},
			wantErr: wantErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			profiler := mock.NewMockAppProfiler(ctrl)
			clock := mock.NewMockClock(ctrl)
			tt.mock(profiler, clock)
			store := kvs.NewInMemoryKVS()
			sessions := accessor.NewSessionAccessor(store)
			observations := accessor.NewObservationAccessor(store)
			issues := accessor.NewIssueBookAccessor(store)
			require.NoError(t, observations.Put(domain.Observation{Second: 2, Stats: domain.Bucket{Waiting: tt.waiting}}))
			sessions.UpdateSession(func(s *domain.Session) {
				s.App.State, s.Collected = tt.state, tt.collected
				s.Observed.Started, s.Observed.Until, s.Observed.Clock = true, 3*domain.BucketSize, clockSync
				if tt.shuttingDown {
					s.Observed.AddMarks(domain.TraceMark{At: time.Second, Message: domain.MarkShutdown})
				}
			})

			uc := usecase.NewUseCase(sessions, observations, issues, nil, nil, nil, profiler, nil, clock).NewCollectAppObservationsUsecase()
			out, err := uc.Exec(ctx, usecase.CollectAppObservationsUsecaseInput{})

			var pending []domain.PendingLeakCheck
			issues.ViewIssueBook(func(b *domain.IssueBook) { pending = b.Pending })
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, tt.collected, sessions.Session().Collected)
				assert.Empty(t, pending)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
			assert.Equal(t, tt.wantPending, pending)
			if out.TookLeaks {
				assert.Equal(t, domain.Collected{Leaks: now, LeakWaiting: tt.waiting}, sessions.Session().Collected)
			}
		})
	}
}
