package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestForwardLogLineUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	received := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	self := domain.TargetInstance{ID: "self", Instance: observation.Instance{Service: "s", Name: "i", Incarnation: "1"}}
	input := func(raw string) usecase.ForwardLogLineUsecaseInput {
		return usecase.ForwardLogLineUsecaseInput{Agent: "agent-1", Source: observation.LogSourceStdin, Raw: raw, ReceivedAt: received}
	}
	tests := []struct {
		name     string
		targets  []domain.TargetInstance
		lines    []string
		want     usecase.ForwardLogLineUsecaseOutput
		wantLogs []observation.LogLine
	}{
		{
			name:    "numbers the lines per instance and buffers them",
			targets: []domain.TargetInstance{self},
			lines:   []string{`{"level":"INFO","msg":"a"}`, `{"level":"ERROR","msg":"b"}`},
			want:    usecase.ForwardLogLineUsecaseOutput{Instance: self.Instance},
			wantLogs: []observation.LogLine{
				{Agent: "agent-1", Seq: 1, At: received, Level: observation.LogLevelInfo, Message: "a", Raw: `{"level":"INFO","msg":"a"}`, Source: observation.LogSourceStdin},
				{Agent: "agent-1", Seq: 2, At: received, Level: observation.LogLevelError, Message: "b", Raw: `{"level":"ERROR","msg":"b"}`, Source: observation.LogSourceStdin},
			},
		},
		{
			name:  "drops a line while no app is observed",
			lines: []string{"hello"},
			want:  usecase.ForwardLogLineUsecaseOutput{Dropped: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store, buf := newStores(t)
			require.NoError(t, store.UpdateTargets(ctx, func([]domain.TargetInstance) []domain.TargetInstance { return tt.targets }))
			uc := usecase.NewUseCase(store, nil, nil, store, nil, nil, buf, nil, nil, nil).NewForwardLogLineUsecase()

			var out usecase.ForwardLogLineUsecaseOutput
			for _, l := range tt.lines {
				var err error
				out, err = uc.Exec(ctx, input(l))
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, out)
			got, err := buf.Buffered(ctx, self.Instance)
			require.NoError(t, err)
			assert.Equal(t, tt.wantLogs, got.Logs)
		})
	}
}
