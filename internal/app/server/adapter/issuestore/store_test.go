package issuestore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/adapter/issuestore"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestStore_FlushAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	occ := func(fp string, seen time.Time) domain.IssueOccurrence {
		return domain.IssueOccurrence{Fingerprint: fp, Summary: "stuck", SeenAt: seen}
	}

	s, err := issuestore.NewStore(dir, at)
	require.NoError(t, err)
	s.Add(occ("LEAK:a", at), occ("LEAK:a", at.Add(time.Minute)), occ("CPU_WAIT_SPIKE", at.Add(2*time.Minute)))
	require.NoError(t, s.Flush(at.Add(2*time.Minute)))

	reloaded, err := issuestore.NewStore(dir, at.Add(time.Hour))
	require.NoError(t, err)

	assert.Len(t, reloaded.Range(at, at.Add(time.Hour)), 3)
	last, ok := reloaded.Last("LEAK:a")
	require.True(t, ok)
	assert.Equal(t, at.Add(time.Minute), last.SeenAt)
	first, ok := reloaded.FirstSeen("LEAK:a")
	require.True(t, ok)
	assert.Equal(t, at, first)
}

func TestStore_Prune(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	now := old.Add(40 * 24 * time.Hour)

	s, err := issuestore.NewStore(dir, old)
	require.NoError(t, err)
	s.Add(domain.IssueOccurrence{Fingerprint: "LEAK:a", SeenAt: old})
	require.NoError(t, s.Flush(old))
	require.NoError(t, s.Flush(now))

	_, ok := s.Last("LEAK:a")
	assert.False(t, ok, "an issue older than the retention is dropped")
	reloaded, err := issuestore.NewStore(dir, now)
	require.NoError(t, err)
	assert.Empty(t, reloaded.Range(old, now), "and its file is removed")
}
