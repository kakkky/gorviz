package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/adapter/issuestore"
	"github.com/kakkky/gorviz/internal/app/server/adapter/kvs"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

func TestGetIssueHistoryUsecase_Exec(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	issues, err := issuestore.NewStore("", now)
	require.NoError(t, err)
	seen := func(fp string, ago time.Duration) domain.IssueOccurrence {
		return domain.IssueOccurrence{Fingerprint: fp, Summary: "stuck", SeenAt: now.Add(-ago), At: now.Add(-ago)}
	}
	issues.Add(seen("LEAK:a", 2*time.Hour), seen("LEAK:a", 10*time.Minute), seen("LEAK:a", 9*time.Minute), seen("LEAK:a", time.Minute), seen("LEAK:b", time.Minute))
	traces := &fakeTraces{}
	uc := usecase.NewUseCase(usecase.Settings{}, nil, nil, kvs.NewInMemoryKVS(), traces, nil, nil, nil, issues, nil)

	out, err := uc.NewGetIssueHistoryUsecase().Exec(context.Background(), usecase.GetIssueHistoryUsecaseInput{Fingerprint: "LEAK:a", To: now, Span: 15 * time.Minute, Now: now})
	require.NoError(t, err)

	assert.Equal(t, time.Minute, out.Step)
	require.True(t, out.Found)
	assert.Equal(t, 3, out.Summary.Count, "only the occurrences in the range")
	assert.Equal(t, now.Add(-2*time.Hour), out.FirstSeen, "first seen is over what is kept, not the range")
	require.Len(t, out.Occurrences, 3)
	assert.Equal(t, now.Add(-time.Minute), out.Occurrences[0].SeenAt, "newest first")
	assert.False(t, out.Occurrences[0].Replayable, "nothing is recorded")
	total := 0
	for _, s := range out.Steps {
		total += s.Count
	}
	assert.Equal(t, 3, total)
	assert.Len(t, out.Steps, 15)
}
