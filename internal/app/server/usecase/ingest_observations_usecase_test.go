package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/testhelper/mock"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestIngestObservationsUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantErr := errors.New("boom")
	now := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	inst := observation.Instance{Service: "checkout", Name: "checkout-1", Incarnation: "inc-1"}
	started := domain.LifecycleEvent{Instance: inst.Name, Incarnation: inst.Incarnation, Kind: domain.LifecycleStart}
	chunk := func(at time.Time) observation.TraceChunk {
		return observation.TraceChunk{StartedAt: at, Duration: 5 * time.Second}
	}
	held := domain.Holding{Cursor: observation.Cursor{LogAgent: "agent-1", LogSeq: 10, TraceChunk: now.Add(-10 * time.Second)}, TraceEnd: now.Add(-5 * time.Second), Latest: now.Add(-5 * time.Second)}
	retention := domain.Retention{Logs: 24 * time.Hour, ExecutionTraces: 24 * time.Hour, GoroutineSnapshots: 24 * time.Hour, GoroutineLeakChecks: 24 * time.Hour}

	type mocks struct {
		locker    *mock.MockInstanceLocker
		holdings  *mock.MockHoldingAccessor
		lifecycle *mock.MockLifecycleAccessor
		logs      *mock.MockLogAccessor
		traces    *mock.MockTraceAccessor
	}
	held1 := func(m mocks) {
		m.locker.EXPECT().LockInstance(inst.Service, inst.Name).Return(func() {})
		m.holdings.EXPECT().Holding(ctx, inst.Service, inst.Name).Return(held, nil)
		m.lifecycle.EXPECT().LastLifecycle(ctx, inst.Service, inst.Name).Return(started, true, nil)
	}
	tests := []struct {
		name    string
		batch   observation.Batch
		mock    func(m mocks)
		want    observation.Cursor
		wantErr error
	}{
		{
			name: "writes only what is not held yet and returns the position on disk",
			batch: observation.Batch{Instance: inst, After: observation.Cursor{LogAgent: "agent-1", LogSeq: 9, TraceChunk: now.Add(-15 * time.Second)},
				Logs:        []observation.LogLine{{Agent: "agent-1", Seq: 10, At: now}, {Agent: "agent-1", Seq: 11, At: now}},
				TraceChunks: []observation.TraceChunk{chunk(now.Add(-10 * time.Second)), chunk(now.Add(-5 * time.Second))}},
			mock: func(m mocks) {
				held1(m)
				m.logs.EXPECT().AppendLogs(ctx, inst, gomock.Any()).DoAndReturn(func(_ context.Context, _ observation.Instance, ls []observation.LogLine) error {
					require.Len(t, ls, 1)
					assert.Equal(t, uint64(11), ls[0].Seq)
					return nil
				})
				m.traces.EXPECT().AppendTraceChunks(ctx, inst, gomock.Any()).DoAndReturn(func(_ context.Context, _ observation.Instance, cs []observation.TraceChunk) error {
					require.Len(t, cs, 1)
					assert.Equal(t, now.Add(-5*time.Second), cs[0].StartedAt)
					return nil
				})
				m.holdings.EXPECT().RememberHolding(ctx, inst.Service, inst.Name, gomock.Any())
			},
			want: observation.Cursor{LogAgent: "agent-1", LogSeq: 11, TraceChunk: now.Add(-5 * time.Second)},
		},
		{
			name: "does not write past a gap and returns only the held position",
			batch: observation.Batch{Instance: inst, After: observation.Cursor{LogAgent: "agent-1", LogSeq: 20, TraceChunk: now},
				Logs: []observation.LogLine{{Agent: "agent-1", Seq: 21, At: now}}, TraceChunks: []observation.TraceChunk{chunk(now.Add(time.Second))}},
			mock: held1,
			want: held.Cursor,
		},
		{
			name:  "does not write a resent batch twice",
			batch: observation.Batch{Instance: inst, After: observation.Cursor{LogAgent: "agent-1", LogSeq: 8}, Logs: []observation.LogLine{{Agent: "agent-1", Seq: 9, At: now}, {Agent: "agent-1", Seq: 10, At: now}}},
			mock:  held1,
			want:  held.Cursor,
		},
		{
			name:  "records the end of the previous and the start of a restarted instance",
			batch: observation.Batch{Instance: observation.Instance{Service: inst.Service, Name: inst.Name, Incarnation: "inc-2"}},
			mock: func(m mocks) {
				m.locker.EXPECT().LockInstance(inst.Service, inst.Name).Return(func() {})
				m.holdings.EXPECT().Holding(ctx, inst.Service, inst.Name).Return(held, nil)
				m.lifecycle.EXPECT().LastLifecycle(ctx, inst.Service, inst.Name).Return(started, true, nil)
				m.lifecycle.EXPECT().AppendLifecycle(ctx, inst.Service, gomock.Any()).DoAndReturn(func(_ context.Context, _ observation.ServiceName, es []domain.LifecycleEvent) error {
					require.Len(t, es, 2)
					assert.Equal(t, domain.LifecycleEnd, es[0].Kind)
					assert.Equal(t, observation.Incarnation("inc-1"), es[0].Incarnation)
					assert.Equal(t, domain.LifecycleStart, es[1].Kind)
					assert.True(t, es[1].Restart)
					return nil
				})
			},
			want: held.Cursor,
		},
		{
			name:  "forgets the held position when a write fails",
			batch: observation.Batch{Instance: inst, Logs: []observation.LogLine{{Agent: "agent-1", Seq: 11, At: now}}},
			mock: func(m mocks) {
				held1(m)
				m.logs.EXPECT().AppendLogs(ctx, inst, gomock.Any()).Return(wantErr)
				m.holdings.EXPECT().ForgetHolding(ctx, inst.Service, inst.Name)
			},
			wantErr: wantErr,
		},
		{
			name:    "refuses a batch without an instance name",
			batch:   observation.Batch{Instance: observation.Instance{Service: "checkout"}},
			mock:    func(m mocks) {},
			wantErr: domain.ErrInvalidArgument,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			m := mocks{locker: mock.NewMockInstanceLocker(ctrl), holdings: mock.NewMockHoldingAccessor(ctrl), lifecycle: mock.NewMockLifecycleAccessor(ctrl),
				logs: mock.NewMockLogAccessor(ctrl), traces: mock.NewMockTraceAccessor(ctrl)}
			tt.mock(m)

			uc := usecase.NewUseCase(usecase.Settings{Retention: retention}, m.locker, m.holdings, nil, m.logs, m.traces, nil, nil, m.lifecycle,
				nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).NewIngestObservationsUsecase()
			out, err := uc.Exec(ctx, usecase.IngestObservationsUsecaseInput{Batches: []observation.Batch{tt.batch}, Now: now})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.IngestObservationsUsecaseOutput{}, out)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out.Cursors[tt.batch.Instance])
		})
	}
}
