package recording_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/adapter/recording"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
)

func TestTraceStore(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	chunk := func(startSec int, events int, marks ...domain.TraceLogMark) domain.TraceChunk {
		return domain.TraceChunk{
			StartedAt: base.Add(time.Duration(startSec) * time.Second),
			Timeline:  domain.SchedulerTimeline{Window: 2 * time.Second, Events: make([]domain.SchedEvent, events)},
			LogMarks:  marks,
		}
	}

	t.Run("success: chunks older than the retention are dropped", func(t *testing.T) {
		t.Parallel()
		s := recording.NewTraceStore(&config.Config{Retention: 5 * time.Second, MaxRecordedEvents: 1000})
		s.Append(chunk(0, 1))
		s.Append(chunk(2, 1))
		s.Append(chunk(4, 1))
		s.Append(chunk(6, 1))

		_, ok := s.At(base.Add(time.Second))
		assert.False(t, ok)
		got, ok := s.At(base.Add(3 * time.Second))
		assert.True(t, ok)
		assert.Equal(t, base.Add(2*time.Second), got.StartedAt)
	})

	t.Run("success: oldest chunks are dropped when events exceed the limit, keeping the latest", func(t *testing.T) {
		t.Parallel()
		s := recording.NewTraceStore(&config.Config{Retention: time.Hour, MaxRecordedEvents: 10})
		s.Append(chunk(0, 6))
		s.Append(chunk(2, 6))
		s.Append(chunk(4, 20))

		_, ok := s.At(base.Add(3 * time.Second))
		assert.False(t, ok)
		latest, ok := s.Latest()
		assert.True(t, ok)
		assert.Equal(t, base.Add(4*time.Second), latest.StartedAt)
	})

	t.Run("success: log marks are found by seq", func(t *testing.T) {
		t.Parallel()
		s := recording.NewTraceStore(&config.Config{Retention: time.Hour, MaxRecordedEvents: 1000})
		s.Append(chunk(0, 1, domain.TraceLogMark{Seq: 1, Goroutine: 7, At: time.Second}))
		s.Append(chunk(2, 1, domain.TraceLogMark{Seq: 2, Goroutine: 8, At: 500 * time.Millisecond}))

		c, m, ok := s.FindLogMark(2)
		assert.True(t, ok)
		assert.Equal(t, base.Add(2*time.Second), c.StartedAt)
		assert.Equal(t, domain.TraceLogMark{Seq: 2, Goroutine: 8, At: 500 * time.Millisecond}, m)
		_, _, ok = s.FindLogMark(3)
		assert.False(t, ok)
	})
}

func TestTraceStore_Between(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	chunk := func(start time.Duration) domain.TraceChunk {
		return domain.TraceChunk{StartedAt: base.Add(start), Timeline: domain.SchedulerTimeline{Window: 2 * time.Second}}
	}
	s := recording.NewTraceStore(&config.Config{Retention: time.Hour, MaxRecordedEvents: 1000})
	for _, start := range []time.Duration{0, 2 * time.Second, 4 * time.Second} {
		s.Append(chunk(start))
	}

	got := s.Between(base.Add(1500*time.Millisecond), base.Add(2500*time.Millisecond))

	assert.Equal(t, []domain.TraceChunk{chunk(0), chunk(2 * time.Second)}, got, "chunks overlapping the interval are returned oldest first")
}
