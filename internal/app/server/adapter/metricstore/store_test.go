package metricstore_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/adapter/metricstore"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestStore_FlushAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	gc := func(v float64) domain.MetricPoint {
		p := domain.MetricPoint{At: at}
		p.GCPerSecond.Observe(v)
		return p
	}

	s, err := metricstore.NewStore(dir, at)
	require.NoError(t, err)
	s.Add(gc(2))
	require.NoError(t, s.Flush(at.Add(5*time.Second)), "an open step is not written yet")
	require.NoError(t, s.Flush(at.Add(domain.MetricStep)))
	s.Add(gc(3))
	require.NoError(t, s.Flush(at.Add(domain.MetricStep)), "a late addition to a written step is written as another delta")

	reloaded, err := metricstore.NewStore(dir, at.Add(time.Minute))
	require.NoError(t, err)
	got := reloaded.Range(at, at.Add(time.Minute), domain.MetricStep)

	require.Len(t, got, 1)
	assert.Equal(t, 2, got[0].GCPerSecond.N, "both deltas are merged when reloaded")
	assert.InDelta(t, 5, got[0].GCPerSecond.Sum, 0.001)
	since, ok := reloaded.Since()
	require.True(t, ok)
	assert.True(t, since.Equal(at))
}

func TestStore_Range_coarseTier(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	s, err := metricstore.NewStore("", now)
	require.NoError(t, err)
	s.Add(domain.MetricPoint{At: now.Add(-3*24*time.Hour + 2*time.Minute + 30*time.Second), HeapGoal: domain.Gauge{Sum: 1, N: 1, Max: 1}})

	assert.Empty(t, s.Range(now.Add(-time.Hour), now, domain.MetricStep))
	got := s.Range(now.Add(-7*24*time.Hour), now, 2*time.Hour)
	require.Len(t, got, 1, "a span longer than the fine tier keeps is read from the coarse tier")
	assert.Equal(t, now.Add(-3*24*time.Hour).Truncate(5*time.Minute), got[0].At)
	since, _ := s.Since()
	assert.True(t, since.Equal(now.Add(-3*24*time.Hour+2*time.Minute+30*time.Second)), "since is the time added, not the start of the coarse step")
}

func TestStore_Since_reload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	at := time.Date(2026, 11, 16, 12, 3, 20, 0, time.UTC)
	s, err := metricstore.NewStore(dir, at)
	require.NoError(t, err)
	s.Add(domain.MetricPoint{At: at, HeapGoal: domain.Gauge{Sum: 1, N: 1, Max: 1}})
	require.NoError(t, s.Flush(at.Add(10*time.Minute)))

	reloaded, err := metricstore.NewStore(dir, at.Add(10*time.Minute))
	require.NoError(t, err)
	since, ok := reloaded.Since()

	require.True(t, ok)
	assert.True(t, since.Equal(at.Truncate(domain.MetricStep)), "the coarse step (12:00) overlapping the fine one does not move since earlier")
}
