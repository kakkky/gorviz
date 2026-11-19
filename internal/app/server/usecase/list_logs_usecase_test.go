package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/adapter/kvs"
	"github.com/kakkky/gorviz/internal/app/server/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

func TestListLogsUsecase_Exec_Histogram(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 19, 1, 0, 0, 0, time.UTC)
	logs := recording.NewLogStore(10)
	logs.Append(model.LogLine{At: base.Add(10 * time.Minute), Level: model.LogLevelInfo, Message: "a"})
	logs.Append(model.LogLine{At: base.Add(10*time.Minute + 30*time.Second), Level: model.LogLevelError, Message: "b"})
	logs.Append(model.LogLine{At: base.Add(50 * time.Minute), Level: model.LogLevelInfo, Message: "c"})
	uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), &fakeTraces{}, logs, nil, nil, nil, nil)

	out, err := uc.NewListLogsUsecase().Exec(context.Background(), usecase.ListLogsUsecaseInput{From: base, To: base.Add(time.Hour), Limit: 10})
	require.NoError(t, err)

	// 1 時間を 60 個に収まる切りのよい長さ (5 分) で、行のない時間も含めて区切る
	assert.Equal(t, 5*time.Minute, out.Step)
	require.Len(t, out.Histogram, 13)
	assert.Equal(t, base, out.Histogram[0].Start)
	assert.Equal(t, map[model.LogLevel]int{model.LogLevelInfo: 1, model.LogLevelError: 1}, out.Histogram[2].Counts)
	assert.Equal(t, map[model.LogLevel]int{model.LogLevelInfo: 1}, out.Histogram[10].Counts)
	assert.Empty(t, out.Histogram[0].Counts)
}
