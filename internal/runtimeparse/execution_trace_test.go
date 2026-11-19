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

	"github.com/kakkky/gorviz/internal/app/protocol/model"
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

	chunk, err := buildTraceChunk(&buf, time.Now(), time.Time{})
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

	chunk, err := buildTraceChunk(&buf, time.Now(), time.Time{})
	require.NoError(t, err)

	require.NotEmpty(t, chunk.Timeline.GCEnds)
	last := chunk.Timeline.GCEnds[len(chunk.Timeline.GCEnds)-1]
	assert.Equal(t, samples[0].Value.Uint64(), last.LiveHeapBytes, "the heap objects right before the end of the mark phase is the live heap")
	assert.Equal(t, samples[1].Value.Uint64(), last.HeapGoalBytes, "the goal emitted right after the end of the mark phase is the next heap goal")
	gogc, ok := last.EffectiveGOGC()
	require.True(t, ok)
	assert.InDelta(t, 100, gogc, 30, "with the default GOGC, the goal is about twice the live heap")
}

func TestBuildTraceChunk_Stats(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, trace.Start(&buf))
	var mu sync.Mutex
	locked := make(chan struct{})
	done := make(chan struct{})
	go func() {
		mu.Lock()
		close(locked)
		time.Sleep(20 * time.Millisecond)
		mu.Unlock()
	}()
	go func() {
		<-locked
		mu.Lock()
		mu.Unlock()
		close(done)
	}()
	<-done
	runtime.GC()
	trace.Stop()

	chunk, err := buildTraceChunk(&buf, time.Now(), time.Time{})
	require.NoError(t, err)
	s := chunk.Stats

	assert.Equal(t, chunk.Timeline.Window, s.Window)
	assert.Positive(t, s.Procs)
	assert.Positive(t, s.SchedLatency.N(), "every goroutine that started running after being runnable is counted")
	assert.Positive(t, s.STW.N(), "the forced GC stops the world")
	assert.Positive(t, s.ProcRunning)
	i := slices.IndexFunc(s.Waits, func(w model.WaitSite) bool { return w.Function == "sync.(*Mutex).Lock" })
	require.GreaterOrEqual(t, i, 0, "the lock wait is counted by its site")
	assert.Contains(t, s.Waits[i].Location, "execution_trace_test.go", "the site is the line in the user's code, not inside sync")
	assert.GreaterOrEqual(t, s.Waits[i].Total, 10*time.Millisecond)
	assert.True(t, slices.ContainsFunc(chunk.Timeline.Events, func(e model.SchedEvent) bool {
		return e.WokenBy != 0 && e.WokenByFunction == "sync.(*Mutex).Unlock"
	}), "the goroutine that unlocked is recorded as the one that woke the waiter")
}

func TestBuildTraceChunk_StatsCountFrom(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, trace.Start(&buf))
	time.Sleep(20 * time.Millisecond)
	trace.Stop()

	all, err := buildTraceChunk(bytes.NewReader(buf.Bytes()), time.Now(), time.Time{})
	require.NoError(t, err)
	half, err := buildTraceChunk(bytes.NewReader(buf.Bytes()), time.Now(), all.StartedAt.Add(all.Timeline.Window/2))
	require.NoError(t, err)

	assert.InDelta(t, float64(all.Timeline.Window/2), float64(half.Stats.Window), float64(time.Millisecond), "the part before countFrom is not counted")
	assert.Equal(t, all.Timeline.Window, half.Timeline.Window, "the timeline itself is kept whole")
}
