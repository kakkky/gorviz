package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/testhelper/mock"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
)

func TestFinishAppObservationUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantErr := errors.New("boom")
	files := []domain.CoverageFile{{Name: "covmeta.1", Data: []byte{1}}}

	tests := []struct {
		name    string
		mock    func(tr *mock.MockAppRuntimeTrace, cov *mock.MockAppRuntimeCoverage, gs *mock.MockAppRuntimeGoroutines, s *mock.MockServerSender, c *mock.MockServerConnection)
		wantErr error
	}{
		{
			name: "sends the last leak profile, writes the trace out, then sends the coverage and the bye",
			mock: func(tr *mock.MockAppRuntimeTrace, cov *mock.MockAppRuntimeCoverage, gs *mock.MockAppRuntimeGoroutines, s *mock.MockServerSender, c *mock.MockServerConnection) {
				gomock.InOrder(
					c.EXPECT().BeginFinish().Return(true),
					tr.EXPECT().WriteTraceEvent(domain.AppExitEvent),
					gs.EXPECT().NextLeakCheckSeq().Return(uint64(5)),
					tr.EXPECT().WriteTraceEvent(domain.LeakCheckEvent(5)),
					gs.EXPECT().ReadGoroutineLeakProfile().Return([]byte("profile"), nil),
					s.EXPECT().SendGoroutineLeakProfile(uint64(5), []byte("profile")).Return(nil),
					tr.EXPECT().StopTrace(),
					tr.EXPECT().WaitTraceDrained(),
					cov.EXPECT().ReadCoverage().Return(files, nil),
					s.EXPECT().SendCoverage(files).Return(nil),
					s.EXPECT().SendBye().Return(nil),
					c.EXPECT().Close(),
				)
			},
		},
		{
			name: "waits for the finish already started without doing it again",
			mock: func(tr *mock.MockAppRuntimeTrace, cov *mock.MockAppRuntimeCoverage, gs *mock.MockAppRuntimeGoroutines, s *mock.MockServerSender, c *mock.MockServerConnection) {
				c.EXPECT().BeginFinish().Return(false)
				c.EXPECT().WaitFinished()
			},
		},
		{
			name: "still writes the trace out and says bye when the leak profile fails",
			mock: func(tr *mock.MockAppRuntimeTrace, cov *mock.MockAppRuntimeCoverage, gs *mock.MockAppRuntimeGoroutines, s *mock.MockServerSender, c *mock.MockServerConnection) {
				c.EXPECT().BeginFinish().Return(true)
				tr.EXPECT().WriteTraceEvent(gomock.Any()).Times(2)
				gs.EXPECT().NextLeakCheckSeq().Return(uint64(1))
				gs.EXPECT().ReadGoroutineLeakProfile().Return(nil, wantErr)
				tr.EXPECT().StopTrace()
				tr.EXPECT().WaitTraceDrained()
				cov.EXPECT().ReadCoverage().Return(nil, errors.New("not built with -cover"))
				s.EXPECT().SendBye().Return(nil)
				c.EXPECT().Close()
			},
			wantErr: wantErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			tr := mock.NewMockAppRuntimeTrace(ctrl)
			cov := mock.NewMockAppRuntimeCoverage(ctrl)
			gs := mock.NewMockAppRuntimeGoroutines(ctrl)
			s := mock.NewMockServerSender(ctrl)
			c := mock.NewMockServerConnection(ctrl)
			tt.mock(tr, cov, gs, s, c)

			uc := usecase.NewUseCase(nil, tr, cov, gs, s, c).NewFinishAppObservationUsecase()
			out, err := uc.Exec(ctx, usecase.FinishAppObservationUsecaseInput{})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.FinishAppObservationUsecaseOutput{}, out)
				return
			}
			require.NoError(t, err)
		})
	}
}
