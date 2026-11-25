package usecase_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kakkky/gorviz/internal/app/agent/testhelper/mock"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
)

func TestStreamAppRuntimeTraceUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantErr := errors.New("boom")
	read := func(data string, err error) func([]byte) (int, error) {
		return func(p []byte) (int, error) { return copy(p, data), err }
	}

	tests := []struct {
		name    string
		mock    func(tr *mock.MockAppRuntimeTrace, s *mock.MockServerSender)
		want    usecase.StreamAppRuntimeTraceUsecaseOutput
		wantErr error
	}{
		{
			name: "sends what it read",
			mock: func(tr *mock.MockAppRuntimeTrace, s *mock.MockServerSender) {
				tr.EXPECT().ReadTrace(gomock.Any()).DoAndReturn(read("chunk", nil))
				s.EXPECT().SendTrace([]byte("chunk")).Return(nil)
			},
		},
		{
			name: "sends the last chunk and ends at the end of the trace",
			mock: func(tr *mock.MockAppRuntimeTrace, s *mock.MockServerSender) {
				tr.EXPECT().ReadTrace(gomock.Any()).DoAndReturn(read("tail", io.EOF))
				s.EXPECT().SendTrace([]byte("tail")).Return(nil)
			},
			want: usecase.StreamAppRuntimeTraceUsecaseOutput{Ended: true},
		},
		{
			name: "returns the send error so that reading goes on",
			mock: func(tr *mock.MockAppRuntimeTrace, s *mock.MockServerSender) {
				tr.EXPECT().ReadTrace(gomock.Any()).DoAndReturn(read("chunk", nil))
				s.EXPECT().SendTrace(gomock.Any()).Return(wantErr)
			},
			wantErr: wantErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			tr := mock.NewMockAppRuntimeTrace(ctrl)
			s := mock.NewMockServerSender(ctrl)
			tt.mock(tr, s)

			uc := usecase.NewUseCase(nil, tr, nil, nil, s, nil).NewStreamAppRuntimeTraceUsecase()
			out, err := uc.Exec(ctx, usecase.StreamAppRuntimeTraceUsecaseInput{})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.StreamAppRuntimeTraceUsecaseOutput{}, out)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
		})
	}
}
