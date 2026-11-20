package recording_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

func TestTraceStore(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	chunk := func(startSec int, events int) model.TraceChunk {
		return model.TraceChunk{
			StartedAt: base.Add(time.Duration(startSec) * time.Second),
			Timeline:  model.SchedulerTimeline{Window: 2 * time.Second, Events: make([]model.SchedEvent, events)},
		}
	}
	startsOf := func(cs []model.TraceChunk) []time.Time {
		var out []time.Time
		for _, c := range cs {
			out = append(out, c.StartedAt)
		}
		return out
	}

	t.Run("success: chunks after the last sent one are returned oldest first up to the limit", func(t *testing.T) {
		t.Parallel()
		s := recording.NewTraceStore(1 << 20)
		for _, sec := range []int{0, 2, 4, 6} {
			s.Append(chunk(sec, 1))
		}

		assert.Equal(t, []time.Time{base.Add(2 * time.Second), base.Add(4 * time.Second)}, startsOf(s.Since(base, 2)))
		assert.Empty(t, s.Since(base.Add(6*time.Second), 2))
	})

	t.Run("success: oldest chunks are dropped while the server is away", func(t *testing.T) {
		t.Parallel()
		s := recording.NewTraceStore(1000)
		s.Append(chunk(0, 4))
		s.Append(chunk(2, 4))
		s.Append(chunk(4, 4))

		assert.Equal(t, []time.Time{base.Add(2 * time.Second), base.Add(4 * time.Second)}, startsOf(s.Since(time.Time{}, 10)))
	})

	t.Run("success: the latest chunk is kept even if it alone exceeds the limit", func(t *testing.T) {
		t.Parallel()
		s := recording.NewTraceStore(1000)
		s.Append(chunk(0, 6))
		s.Append(chunk(2, 50))

		assert.Equal(t, []time.Time{base.Add(2 * time.Second)}, startsOf(s.Since(time.Time{}, 10)))
	})
}
