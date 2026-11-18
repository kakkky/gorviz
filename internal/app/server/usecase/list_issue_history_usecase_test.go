package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/adapter/issuestore"
	"github.com/kakkky/gorviz/internal/app/server/adapter/kvs"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

func TestListIssueHistoryUsecase_Exec(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	issues, err := issuestore.NewStore("", now)
	require.NoError(t, err)
	seen := func(kind model.IssueKind, fp string, ago time.Duration) domain.IssueOccurrence {
		return domain.IssueOccurrence{Fingerprint: fp, Kind: kind, Subject: fp, SeenAt: now.Add(-ago)}
	}
	issues.Add(
		seen(model.IssueLeak, "LEAK:a", 10*time.Minute), seen(model.IssueLeak, "LEAK:a", time.Minute),
		seen(model.IssueCPUWaitSpike, "CPU_WAIT_SPIKE", time.Minute),
		seen(model.IssueLeak, "LEAK:a", 2*time.Hour),
	)
	uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), &fakeTraces{}, nil, nil, nil, issues, nil)

	t.Run("success: counts by kind over the range", func(t *testing.T) {
		t.Parallel()
		out, err := uc.NewListIssueHistoryUsecase().Exec(context.Background(), usecase.ListIssueHistoryUsecaseInput{Now: now, Span: 15 * time.Minute})
		require.NoError(t, err)

		assert.Len(t, out.Issues, 2)
		assert.Equal(t, map[model.IssueKind]int{model.IssueLeak: 2, model.IssueCPUWaitSpike: 1}, out.Totals)
		assert.Equal(t, time.Minute, out.Step)
		require.Len(t, out.Steps, 15)
		assert.Equal(t, map[model.IssueKind]int{model.IssueLeak: 1, model.IssueCPUWaitSpike: 1}, out.Steps[14].Counts)
	})

	t.Run("success: the kind filter applies to the trend too", func(t *testing.T) {
		t.Parallel()
		out, err := uc.NewListIssueHistoryUsecase().Exec(context.Background(), usecase.ListIssueHistoryUsecaseInput{Now: now, Span: 15 * time.Minute, Kind: model.IssueCPUWaitSpike})
		require.NoError(t, err)

		assert.Equal(t, map[model.IssueKind]int{model.IssueCPUWaitSpike: 1}, out.Totals)
	})
}
