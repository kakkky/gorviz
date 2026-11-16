package runtimeparse

import (
	"bytes"
	"runtime"
	"runtime/metrics"
	"runtime/trace"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
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
	assert.True(t, slices.ContainsFunc(tl.GC, func(r model.GCRange) bool { return r.Kind == model.GCStopTheWorld && r.End >= r.Start }), "the forced GC stops the world")
	for _, lane := range tl.Procs {
		for _, s := range lane.Spans {
			assert.LessOrEqual(t, s.Start, s.End)
			assert.LessOrEqual(t, s.End, tl.Window)
		}
	}
}

func TestBuildSchedulerTimeline_GCEnds(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, trace.Start(&buf))
	retained := make([][]byte, 0, 1000)
	for i := range 1000 {
		retained = append(retained, make([]byte, 4096+i))
	}
	runtime.GC()
	trace.Stop()
	samples := []metrics.Sample{{Name: "/gc/heap/live:bytes"}, {Name: "/gc/heap/goal:bytes"}}
	metrics.Read(samples)
	runtime.KeepAlive(retained)

	chunk, err := buildTraceChunk(&buf, time.Now())
	require.NoError(t, err)

	require.NotEmpty(t, chunk.Timeline.GCEnds)
	last := chunk.Timeline.GCEnds[len(chunk.Timeline.GCEnds)-1]
	assert.Equal(t, samples[0].Value.Uint64(), last.LiveHeapBytes, "the heap objects right before the end of the mark phase is the live heap")
	assert.Equal(t, samples[1].Value.Uint64(), last.HeapGoalBytes, "the goal emitted right after the end of the mark phase is the next heap goal")
	gogc, ok := last.EffectiveGOGC()
	require.True(t, ok)
	assert.InDelta(t, 100, gogc, 30, "with the default GOGC, the goal is about twice the live heap")
}
