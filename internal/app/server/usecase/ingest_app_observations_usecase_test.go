package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kakkky/gorviz/internal/app/server/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
	"github.com/kakkky/gorviz/internal/app/server/testhelper/mock"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

func TestIngestAppObservationsUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantErr := errors.New("boom")
	profile := []byte("goroutineleak profile: total 2\n2 @ 0x1 0x2\n#\t0x1\tmain.leak.func1+0x27\t/app/main.go:23\n")

	tests := []struct {
		name    string
		input   usecase.IngestAppObservationsUsecaseInput
		mock    func(s *mock.MockTraceStream)
		check   func(t *testing.T, store *kvs.InMemoryKVS)
		wantErr error
	}{
		{
			name:  "keeps what the agent said about the app and the number of Ps",
			input: usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationHello, Hello: domain.AppRun{GoVersion: "go1.27.1", Procs: 6, PID: 42}},
			mock:  func(*mock.MockTraceStream) {},
			check: func(t *testing.T, store *kvs.InMemoryKVS) {
				s := accessor.NewSessionAccessor(store).Session()
				assert.Equal(t, 6, s.App.Procs)
				assert.Equal(t, 42, s.App.PID)
				accessor.NewTraceIndexAccessor(store).ViewTraceIndex(func(ix *domain.TraceIndex) { assert.Equal(t, 6, ix.Procs()) })
			},
		},
		{
			name:  "passes the trace on to the reader",
			input: usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationTrace, Trace: []byte("chunk")},
			mock: func(s *mock.MockTraceStream) {
				s.EXPECT().AppendTrace([]byte("chunk")).Return(nil)
			},
		},
		{
			name:  "returns the error of passing the trace on",
			input: usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationTrace, Trace: []byte("chunk")},
			mock: func(s *mock.MockTraceStream) {
				s.EXPECT().AppendTrace(gomock.Any()).Return(wantErr)
			},
			wantErr: wantErr,
		},
		{
			name:  "keeps a goroutineleak profile until its trace reaches the index",
			input: usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationGoroutineLeakProfile, LeakSeq: 3, LeakText: profile},
			mock:  func(*mock.MockTraceStream) {},
			check: func(t *testing.T, store *kvs.InMemoryKVS) {
				accessor.NewIssueBookAccessor(store).ViewIssueBook(func(b *domain.IssueBook) {
					require.Len(t, b.Pending, 1)
					assert.Equal(t, uint64(3), b.Pending[0].Seq)
					assert.Equal(t, 2, b.Pending[0].Stacks[0].Count)
				})
			},
		},
		{
			name:  "closes the trace as broken when the agent left without saying bye",
			input: usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationEnd, Broken: true},
			mock: func(s *mock.MockTraceStream) {
				s.EXPECT().CloseTrace(true)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			stream := mock.NewMockTraceStream(ctrl)
			tt.mock(stream)
			store := kvs.NewInMemoryKVS()

			uc := usecase.NewUseCase(accessor.NewSessionAccessor(store), accessor.NewTraceIndexAccessor(store), accessor.NewIssueBookAccessor(store), stream, nil, nil, nil, nil, nil, nil).NewIngestAppObservationsUsecase()
			out, err := uc.Exec(ctx, tt.input)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.IngestAppObservationsUsecaseOutput{}, out)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, store)
			}
		})
	}
}
