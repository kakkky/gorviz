package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestRollup(t *testing.T) {
	t.Parallel()
	to := time.Date(2026, 11, 16, 12, 1, 5, 0, time.UTC)
	point := func(at time.Time, goroutines float64) domain.MetricPoint {
		p := domain.MetricPoint{At: at}
		p.States = map[model.GoroutineStateCategory]domain.Gauge{model.GoroutineWaiting: {Sum: goroutines, N: 1, Max: goroutines}}
		p.GCPerSecond.Observe(goroutines / 10)
		return p
	}
	points := []domain.MetricPoint{
		point(time.Date(2026, 11, 16, 11, 59, 50, 0, time.UTC), 90),
		point(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC), 10),
		point(time.Date(2026, 11, 16, 12, 0, 50, 0, time.UTC), 20),
		point(time.Date(2026, 11, 16, 12, 1, 0, 0, time.UTC), 30),
	}

	got := domain.Rollup(points, to, 2*time.Minute, time.Minute)

	require.Len(t, got, 2)
	assert.Equal(t, time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC), got[0].At, "the last step is the one containing to")
	total, ok := got[0].GoroutineTotal()
	require.True(t, ok)
	assert.InDelta(t, 15, total, 0.001, "gauges average over the merged steps; points before the span are left out")
	assert.InDelta(t, 2, got[0].GCPerSecond.Max, 0.001, "the max is kept across the merged steps")
	total, _ = got[1].GoroutineTotal()
	assert.InDelta(t, 30, total, 0.001)
}
func TestMetricStepFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 10*time.Second, domain.MetricStepFor(15*time.Minute, 120))
	assert.Equal(t, 30*time.Second, domain.MetricStepFor(time.Hour, 120))
	assert.Equal(t, 15*time.Minute, domain.MetricStepFor(24*time.Hour, 120))
	assert.Equal(t, 6*time.Hour, domain.MetricStepFor(30*24*time.Hour, 120))
}
