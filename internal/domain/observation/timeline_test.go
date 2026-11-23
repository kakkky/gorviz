package observation_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

const ms = time.Millisecond

func running(at time.Duration, g observation.GoroutineID) observation.StateChange {
	return observation.StateChange{At: at, Kind: observation.StateChangeGoroutine, Goroutine: g, Place: observation.Place{Kind: observation.PlaceRunning, HasProc: true}}
}

func waiting(at time.Duration, g observation.GoroutineID) observation.StateChange {
	return observation.StateChange{At: at, Kind: observation.StateChangeGoroutine, Goroutine: g, Place: observation.Place{Kind: observation.PlaceWaiting, Reason: "sync"}}
}

func TestTraceChunk_Slice(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	chunk := observation.TraceChunk{
		StartedAt: start, Duration: 100 * ms,
		Events:   []observation.StateChange{running(0, 1), waiting(10*ms, 1), running(50*ms, 2), running(90*ms, 1)},
		Procs:    []observation.ProcLane{{Proc: 0, Spans: []observation.ProcSpan{{Goroutine: 1, Start: 0, End: 10 * ms}, {Goroutine: 2, Start: 50 * ms, End: 90 * ms}}}},
		LogMarks: []observation.LogMark{{Seq: 1, Goroutine: 2, At: 60 * ms}},
	}
	tests := []struct {
		name       string
		from, to   time.Duration
		wantStart  time.Time
		wantEvents []observation.StateChange
		wantSpans  []observation.ProcSpan
		wantMarks  []observation.LogMark
	}{
		{
			name: "carries the state at the start as events at time zero",
			from: 20 * ms, to: 70 * ms,
			wantStart:  start.Add(20 * ms),
			wantEvents: []observation.StateChange{waiting(0, 1), running(30*ms, 2)},
			wantSpans:  []observation.ProcSpan{{Goroutine: 2, Start: 30 * ms, End: 50 * ms}},
			wantMarks:  []observation.LogMark{{Seq: 1, Goroutine: 2, At: 40 * ms}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := chunk.Slice(tt.from, tt.to)
			assert.Equal(t, tt.wantStart, got.StartedAt)
			assert.Equal(t, tt.to-tt.from, got.Duration)
			assert.Equal(t, tt.wantEvents, got.Events)
			require.Len(t, got.Procs, 1)
			assert.Equal(t, tt.wantSpans, got.Procs[0].Spans)
			assert.Equal(t, tt.wantMarks, got.LogMarks)
		})
	}
}

func TestJoin(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	first := observation.TraceChunk{StartedAt: start, Duration: 100 * ms, Events: []observation.StateChange{running(0, 1)}, Entries: map[observation.GoroutineID]string{1: "main.a"}, Threads: 3}
	second := observation.TraceChunk{StartedAt: start.Add(110 * ms), Duration: 100 * ms, Events: []observation.StateChange{waiting(0, 1), running(5*ms, 2)}, Entries: map[observation.GoroutineID]string{2: "main.b"}, Threads: 5}
	tests := []struct {
		name       string
		chunks     []observation.TraceChunk
		from, to   time.Time
		wantOK     bool
		wantEvents []observation.StateChange
		wantDur    time.Duration
	}{
		{
			name:   "places each chunk at its own start",
			chunks: []observation.TraceChunk{first, second},
			from:   start.Add(50 * ms), to: start.Add(200 * ms),
			wantOK:     true,
			wantEvents: []observation.StateChange{running(0, 1), waiting(60*ms, 1), running(65*ms, 2)},
			wantDur:    150 * ms,
		},
		{
			name:   "answers nothing outside every chunk",
			chunks: []observation.TraceChunk{first},
			from:   start.Add(time.Second), to: start.Add(2 * time.Second),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := observation.Join(tt.chunks, tt.from, tt.to)
			require.Equal(t, tt.wantOK, ok)
			if !ok {
				return
			}
			assert.Equal(t, tt.wantEvents, got.Events)
			assert.Equal(t, tt.wantDur, got.Duration)
			assert.Equal(t, map[observation.GoroutineID]string{1: "main.a", 2: "main.b"}, got.Entries)
			assert.Equal(t, 5, got.Threads)
		})
	}
}
