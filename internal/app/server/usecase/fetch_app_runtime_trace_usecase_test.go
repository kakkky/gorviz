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
	"github.com/kakkky/gorviz/internal/app/server/driver/tracestream"
	"github.com/kakkky/gorviz/internal/app/server/testhelper/mock"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

func TestFetchAppRuntimeTraceUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	refused := errors.New("connection refused")

	tests := []struct {
		name  string
		mock  func(p *mock.MockAppProfiler, c *mock.MockClock, sessions domain.SessionStore)
		want  []domain.TraceChunk
		count int
	}{
		{
			name: "waits for the app to open pprof, streams the chunks with the threads, and closes when the app exited",
			mock: func(p *mock.MockAppProfiler, c *mock.MockClock, sessions domain.SessionStore) {
				gomock.InOrder(
					p.EXPECT().FetchTrace(gomock.Any(), domain.TraceChunkLength).Return(nil, refused),
					c.EXPECT().Sleep(gomock.Any(), gomock.Any()).Return(nil),
					p.EXPECT().FetchTrace(gomock.Any(), domain.TraceChunkLength).Return([]byte("chunk 1"), nil),
					p.EXPECT().FetchThreadCreateProfile(gomock.Any()).Return([]byte("threadcreate profile: total 7\n"), nil),
					p.EXPECT().FetchTrace(gomock.Any(), domain.TraceChunkLength).Return([]byte("chunk 2"), nil),
					p.EXPECT().FetchThreadCreateProfile(gomock.Any()).Return(nil, refused),
					p.EXPECT().FetchTrace(gomock.Any(), domain.TraceChunkLength).DoAndReturn(func(context.Context, any) ([]byte, error) {
						sessions.UpdateSession(func(s *domain.Session) { s.App.ProcessExited = true })
						return nil, refused
					}),
				)
			},
			want:  []domain.TraceChunk{{Trace: []byte("chunk 1"), Threads: 7}, {Trace: []byte("chunk 2")}},
			count: 2,
		},
		{
			name: "closes the trace without a chunk when the build failed",
			mock: func(p *mock.MockAppProfiler, _ *mock.MockClock, sessions domain.SessionStore) {
				sessions.UpdateSession(func(s *domain.Session) { s.App.State = domain.AppBuildFailed })
				p.EXPECT().FetchTrace(gomock.Any(), domain.TraceChunkLength).Return(nil, refused)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			profiler := mock.NewMockAppProfiler(ctrl)
			clock := mock.NewMockClock(ctrl)
			sessions := accessor.NewSessionAccessor(kvs.NewInMemoryKVS())
			stream := accessor.NewTraceStreamAccessor(tracestream.NewTraceStream())
			tt.mock(profiler, clock, sessions)

			uc := usecase.NewUseCase(sessions, nil, nil, stream, nil, nil, profiler, nil, clock).NewFetchAppRuntimeTraceUsecase()
			out, err := uc.Exec(ctx, usecase.FetchAppRuntimeTraceUsecaseInput{})

			require.NoError(t, err)
			assert.Equal(t, tt.count, out.Chunks)
			var got []domain.TraceChunk
			for {
				c, ok := stream.NextTrace()
				if !ok {
					break
				}
				got = append(got, c)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
