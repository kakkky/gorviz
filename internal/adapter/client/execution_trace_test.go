package client

import (
	"bytes"
	"runtime"
	"runtime/trace"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/domain"
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
	runtime.GC()
	trace.Stop()

	chunk, err := buildTraceChunk(&buf, time.Now())
	require.NoError(t, err)
	tl := chunk.Timeline

	assert.WithinDuration(t, time.Now(), chunk.StartedAt, time.Second, "wall clock of the trace start comes from the clock snapshot")
	assert.Positive(t, tl.Window)
	assert.NotEmpty(t, tl.Procs)
	assert.NotEmpty(t, tl.Threads)
	assert.GreaterOrEqual(t, tl.GoroutinesCreated, 8)
	assert.NotEmpty(t, tl.Heap, "heap size and goal are sampled in the trace")
	assert.Positive(t, tl.GCCycles, "the forced GC is counted")
	assert.True(t, slices.ContainsFunc(tl.GC, func(r domain.GCRange) bool { return r.Kind == domain.GCStopTheWorld && r.End >= r.Start }), "the forced GC stops the world")
	for _, lane := range tl.Procs {
		for _, s := range lane.Spans {
			assert.LessOrEqual(t, s.Start, s.End)
			assert.LessOrEqual(t, s.End, tl.Window)
		}
	}
}
