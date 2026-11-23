package usecase_test

import (
	"context"
	"errors"
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

func TestExportObservationsUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantErr := errors.New("boom")
	agent := observation.AgentID("agent-1")
	in := observation.Instance{Service: "s", Name: "i", Incarnation: "1"}
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	logs := []observation.LogLine{{Agent: agent, Seq: 1}, {Agent: agent, Seq: 2}, {Agent: agent, Seq: 3}}
	chunk := observation.TraceChunk{StartedAt: t0, Duration: 5 * time.Second}
	tests := []struct {
		name     string
		cursor   observation.Cursor
		ended    bool
		mock     func(s *mock.MockServerClient)
		want     usecase.ExportObservationsUsecaseOutput
		wantRest domain.Buffered
		wantErr  error
	}{
		{
			name:   "sends what follows the server's cursor and drops what the server now has",
			cursor: observation.Cursor{LogAgent: agent, LogSeq: 1},
			mock: func(s *mock.MockServerClient) {
				s.EXPECT().Export(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, b []observation.Batch) (map[observation.Instance]observation.Cursor, error) {
					require.Len(t, b, 1)
					assert.Equal(t, observation.Cursor{LogAgent: agent, LogSeq: 1}, b[0].After)
					assert.Equal(t, logs[1:], b[0].Logs)
					assert.Equal(t, []observation.TraceChunk{chunk}, b[0].TraceChunks)
					return map[observation.Instance]observation.Cursor{in: {LogAgent: agent, LogSeq: 2, TraceChunk: t0}}, nil
				})
			},
			want:     usecase.ExportObservationsUsecaseOutput{Sent: 1},
			wantRest: domain.Buffered{Logs: logs[2:]},
		},
		{
			name:  "removes an ended instance once the server has everything",
			ended: true,
			mock: func(s *mock.MockServerClient) {
				s.EXPECT().Export(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, b []observation.Batch) (map[observation.Instance]observation.Cursor, error) {
					assert.True(t, b[0].Ended)
					return map[observation.Instance]observation.Cursor{in: {LogAgent: agent, LogSeq: 3, TraceChunk: t0}}, nil
				})
			},
			want: usecase.ExportObservationsUsecaseOutput{Sent: 1, Settled: []observation.Instance{in}},
		},
		{
			name: "keeps everything when the server cannot be reached",
			mock: func(s *mock.MockServerClient) {
				s.EXPECT().Export(ctx, gomock.Any()).Return(nil, wantErr)
			},
			wantErr: wantErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			s := mock.NewMockServerClient(ctrl)
			tt.mock(s)
			store, buf := newStores(t)
			for _, l := range logs {
				require.NoError(t, buf.PushLogLine(ctx, in, l))
			}
			require.NoError(t, buf.PushTraceChunk(ctx, in, chunk))
			require.NoError(t, store.SetCursor(ctx, in, tt.cursor))
			if tt.ended {
				require.NoError(t, store.MarkEnded(ctx, in))
			}

			uc := usecase.NewUseCase(nil, nil, nil, nil, store, store, buf, nil, nil, s).NewExportObservationsUsecase()
			out, err := uc.Exec(ctx, usecase.ExportObservationsUsecaseInput{Agent: agent})

			rest, berr := buf.Buffered(ctx, in)
			require.NoError(t, berr)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.ExportObservationsUsecaseOutput{}, out)
				assert.Len(t, rest.Logs, len(logs))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
			assert.Equal(t, tt.wantRest, rest)
			ended, err := store.EndedInstances(ctx)
			require.NoError(t, err)
			assert.Empty(t, ended)
		})
	}
}
