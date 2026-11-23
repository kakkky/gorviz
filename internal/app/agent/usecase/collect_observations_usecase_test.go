package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime/trace"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/testhelper/mock"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

var rawTrace []byte

func TestMain(m *testing.M) {
	var buf bytes.Buffer
	if err := trace.Start(&buf); err != nil {
		panic(err)
	}
	done := make(chan struct{})
	go func() { <-done }()
	time.Sleep(10 * time.Millisecond)
	trace.Stop()
	close(done)
	rawTrace = buf.Bytes()
	os.Exit(m.Run())
}

func TestCollectObservationsUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantErr := errors.New("boom")
	self := domain.TargetInstance{ID: "self", Instance: observation.Instance{Service: "s", Name: "i", Incarnation: "1"}, PprofAddr: "localhost:6060"}
	in := usecase.CollectObservationsUsecaseInput{Mode: domain.ModeSidecar, ChunkLength: 5 * time.Second, LeakCheckInterval: time.Minute}
	leakProfile := []byte("goroutineleak profile: total 1\n1 @ 0x1 0x2\n#\t0x1\tmain.leak+0x2f\t/app/main.go:9\n")
	threads := []byte("threadcreate profile: total 9\n")
	receivedAt := time.Now()
	tests := []struct {
		name         string
		input        usecase.CollectObservationsUsecaseInput
		state        domain.TargetState
		mock         func(r *mock.MockRuntimeClient)
		want         usecase.CollectObservationsUsecaseOutput
		wantInstance observation.Instance
		wantLeak     observation.LeakCheckStatus
		wantEnded    []observation.Instance
		wantErr      error
	}{
		{
			name:  "buffers the chunk, the snapshot and the leak check of the app",
			input: in,
			mock: func(r *mock.MockRuntimeClient) {
				r.EXPECT().FetchExecutionTrace(gomock.Any(), self, 5*time.Second).Return(domain.RawTrace{Data: rawTrace, ReceivedAt: receivedAt}, nil)
				r.EXPECT().FetchThreadCreateProfile(gomock.Any(), self).Return(threads, nil)
				r.EXPECT().FetchGoroutineLeakProfile(gomock.Any(), self).Return(leakProfile, nil)
			},
			want:         usecase.CollectObservationsUsecaseOutput{Collected: 1},
			wantInstance: self.Instance,
			wantLeak:     observation.LeakCheckDone,
		},
		{
			name:  "reports the leak check as unsupported when the app's Go has no goroutineleak profile",
			input: in,
			mock: func(r *mock.MockRuntimeClient) {
				r.EXPECT().FetchExecutionTrace(gomock.Any(), self, 5*time.Second).Return(domain.RawTrace{Data: rawTrace, ReceivedAt: receivedAt}, nil)
				r.EXPECT().FetchThreadCreateProfile(gomock.Any(), self).Return(threads, nil)
				r.EXPECT().FetchGoroutineLeakProfile(gomock.Any(), self).Return(nil, domain.ErrNotFound.With("no profile"))
			},
			want:         usecase.CollectObservationsUsecaseOutput{Collected: 1},
			wantInstance: self.Instance,
			wantLeak:     observation.LeakCheckUnsupported,
		},
		{
			name:  "skips the leak check until the interval passes",
			input: in,
			state: domain.TargetState{Instance: self.Instance, LastLeakCheck: receivedAt.Add(-time.Second)},
			mock: func(r *mock.MockRuntimeClient) {
				r.EXPECT().FetchExecutionTrace(gomock.Any(), self, 5*time.Second).Return(domain.RawTrace{Data: rawTrace, ReceivedAt: receivedAt}, nil)
				r.EXPECT().FetchThreadCreateProfile(gomock.Any(), self).Return(threads, nil)
			},
			want:         usecase.CollectObservationsUsecaseOutput{Collected: 1},
			wantInstance: self.Instance,
		},
		{
			name:  "starts a new instance when the sidecar app's max goroutine ID decreased",
			input: in,
			state: domain.TargetState{Instance: self.Instance, MaxAliveGoroutineID: 1 << 40, LastLeakCheck: receivedAt},
			mock: func(r *mock.MockRuntimeClient) {
				r.EXPECT().FetchExecutionTrace(gomock.Any(), self, 5*time.Second).Return(domain.RawTrace{Data: rawTrace, ReceivedAt: receivedAt}, nil)
				r.EXPECT().FetchThreadCreateProfile(gomock.Any(), self).Return(threads, nil)
				r.EXPECT().FetchGoroutineLeakProfile(gomock.Any(), gomock.Any()).Return(leakProfile, nil)
			},
			want:         usecase.CollectObservationsUsecaseOutput{Collected: 1},
			wantInstance: observation.Instance{Service: "s", Name: "i", Incarnation: domain.NewIncarnation(receivedAt)},
			wantLeak:     observation.LeakCheckDone,
			wantEnded:    []observation.Instance{self.Instance},
		},
		{
			name:  "returns the error and buffers nothing when the trace cannot be taken",
			input: in,
			mock: func(r *mock.MockRuntimeClient) {
				r.EXPECT().FetchExecutionTrace(gomock.Any(), self, 5*time.Second).Return(domain.RawTrace{}, wantErr)
			},
			wantErr:      wantErr,
			wantInstance: self.Instance,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			r := mock.NewMockRuntimeClient(ctrl)
			tt.mock(r)
			store, buf := newStores(t)
			require.NoError(t, store.UpdateTargets(ctx, func([]domain.TargetInstance) []domain.TargetInstance { return []domain.TargetInstance{self} }))
			require.NoError(t, store.SetTargetState(ctx, self.ID, tt.state))

			uc := usecase.NewUseCase(store, store, store, nil, nil, store, buf, r, nil, nil).NewCollectObservationsUsecase()
			out, err := uc.Exec(ctx, tt.input)

			got, berr := buf.Buffered(ctx, tt.wantInstance)
			require.NoError(t, berr)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.CollectObservationsUsecaseOutput{}, out)
				assert.True(t, got.Empty())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
			require.Len(t, got.TraceChunks, 1)
			assert.Equal(t, 9, got.TraceChunks[0].Threads)
			require.Len(t, got.Snapshots, 1)
			assert.Equal(t, got.TraceChunks[0].EndedAt(), got.Snapshots[0].At)
			assert.NotEmpty(t, got.Snapshots[0].Goroutines)
			if tt.wantLeak == "" {
				assert.Empty(t, got.LeakChecks)
			} else {
				require.Len(t, got.LeakChecks, 1)
				assert.Equal(t, tt.wantLeak, got.LeakChecks[0].Status)
			}
			targets, err := store.Targets(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.wantInstance, targets[0].Instance)
			ended, err := store.EndedInstances(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEnded, ended)
			memory, err := store.GoroutineMemory(ctx, tt.wantInstance)
			require.NoError(t, err)
			assert.Len(t, memory, len(got.Snapshots[0].Goroutines))
		})
	}
}
