package client

import (
	"bytes"
	"runtime/trace"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSchedulerTimeline(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, trace.Start(&buf))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			deadline := time.Now().Add(5 * time.Millisecond)
			for time.Now().Before(deadline) {
			}
		})
	}
	wg.Wait()
	trace.Stop()

	chunk, err := buildTraceChunk(&buf, time.Now())
	require.NoError(t, err)
	tl := chunk.Timeline

	assert.WithinDuration(t, time.Now(), chunk.StartedAt, time.Second, "wall clock of the trace start comes from the clock snapshot")
	assert.Positive(t, tl.Window)
	assert.NotEmpty(t, tl.Procs)
	assert.NotEmpty(t, tl.Threads)
	assert.GreaterOrEqual(t, tl.GoroutinesCreated, 8)
	for _, lane := range tl.Procs {
		for _, s := range lane.Spans {
			assert.LessOrEqual(t, s.Start, s.End)
			assert.LessOrEqual(t, s.End, tl.Window)
		}
	}
}
