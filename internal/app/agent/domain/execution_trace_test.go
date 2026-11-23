package domain_test

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

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
)

// captureTrace は f の間の execution trace を取る。trace はプロセスで 1 本なので、このファイルのテストは並列にしない
func captureTrace(t *testing.T, f func()) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, trace.Start(&buf))
	f()
	trace.Stop()
	return buf.Bytes()
}

func blockForever(ch chan struct{}) { <-ch }

func TestParseExecutionTrace(t *testing.T) {
	stuck := make(chan struct{})
	defer close(stuck)
	go blockForever(stuck)
	time.Sleep(5 * time.Millisecond)

	var mu sync.Mutex
	raw := captureTrace(t, func() {
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				deadline := time.Now().Add(3 * time.Millisecond)
				for time.Now().Before(deadline) {
				}
			})
		}
		wg.Wait()
		mu.Lock()
		waiterDone := make(chan struct{})
		go func() {
			mu.Lock()
			mu.Unlock()
			close(waiterDone)
		}()
		time.Sleep(5 * time.Millisecond)
		mu.Unlock()
		<-waiterDone
		loggerhook.Mark(t.Context())
		runtime.GC()
	})

	tests := []struct {
		name  string
		check func(t *testing.T, p domain.ParsedTrace)
	}{
		{
			name: "builds the chunk on the wall clock",
			check: func(t *testing.T, p domain.ParsedTrace) {
				c := p.Chunk
				assert.WithinDuration(t, time.Now(), c.StartedAt, time.Second)
				assert.Positive(t, c.Duration)
				assert.NotEmpty(t, c.Procs)
				assert.GreaterOrEqual(t, c.GoroutinesCreated, 5)
				assert.NotEmpty(t, c.Heap)
				assert.Positive(t, c.GCCycles)
				assert.Len(t, c.LogMarks, 1)
				assert.True(t, slices.ContainsFunc(c.GC, func(r observation.GCRange) bool { return r.Kind == observation.GCStopTheWorld }))
				for _, l := range c.Procs {
					for _, s := range l.Spans {
						assert.LessOrEqual(t, s.Start, s.End)
						assert.LessOrEqual(t, s.End, c.Duration)
					}
				}
			},
		},
		{
			name: "counts the waits by their site in the user's code",
			check: func(t *testing.T, p domain.ParsedTrace) {
				s := p.Chunk.Stats
				assert.Positive(t, s.Procs)
				assert.Positive(t, s.SchedLatency.N())
				assert.Positive(t, s.STW.N())
				i := slices.IndexFunc(s.Waits, func(w observation.WaitSite) bool { return w.Function == "sync.(*Mutex).Lock" })
				require.GreaterOrEqual(t, i, 0)
				assert.Contains(t, s.Waits[i].Location, "execution_trace_test.go")
				assert.True(t, slices.ContainsFunc(p.Chunk.Events, func(e observation.StateChange) bool {
					return e.WokenBy != 0 && e.WokenByFunction == "sync.(*Mutex).Unlock"
				}))
			},
		},
		{
			name: "keeps the end state and the stack of a goroutine that waited since before the chunk",
			check: func(t *testing.T, p domain.ParsedTrace) {
				var found bool
				for _, g := range p.Goroutines {
					if g.Alive && observation.EntryFunction(g.Stack) == "github.com/kakkky/gorviz/internal/app/agent/domain_test.blockForever" {
						found = true
						assert.Equal(t, observation.GoroutineWaiting, g.State)
						assert.False(t, g.Determined)
						assert.Equal(t, "chan receive", domain.InferWaitReason(g.Stack))
					}
				}
				assert.True(t, found)
				assert.Positive(t, p.MaxGoroutineID)
			},
		},
		{
			name: "remembers the creator of goroutines created in the chunk",
			check: func(t *testing.T, p domain.ParsedTrace) {
				assert.True(t, slices.ContainsFunc(sliceOf(p.Goroutines), func(g domain.GoroutineAtEnd) bool {
					return g.HasCreatedBy && g.CreatedBy.Line > 0
				}))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := domain.ParseExecutionTrace(raw, time.Now(), time.Time{})
			require.NoError(t, err)
			tt.check(t, p)
		})
	}
}

func TestParseExecutionTrace_DropsBeforeSince(t *testing.T) {
	raw := captureTrace(t, func() { time.Sleep(20 * time.Millisecond) })
	all, err := domain.ParseExecutionTrace(raw, time.Now(), time.Time{})
	require.NoError(t, err)
	tests := []struct {
		name  string
		since time.Time
		want  time.Duration
	}{
		{name: "keeps the whole chunk without a previous chunk", want: all.Chunk.Duration},
		{name: "drops the part overlapping the previous chunk", since: all.Chunk.StartedAt.Add(all.Chunk.Duration / 2), want: all.Chunk.Duration - all.Chunk.Duration/2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := domain.ParseExecutionTrace(raw, time.Now(), tt.since)
			require.NoError(t, err)
			assert.InDelta(t, float64(tt.want), float64(p.Chunk.Duration), float64(time.Microsecond))
			if !tt.since.IsZero() {
				assert.WithinDuration(t, tt.since, p.Chunk.StartedAt, time.Microsecond)
			}
		})
	}
}

func TestParseExecutionTrace_RejectsBrokenInput(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "rejects bytes that are not a trace", raw: []byte("not a trace")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := domain.ParseExecutionTrace(tt.raw, time.Now(), time.Time{})
			require.ErrorIs(t, err, domain.ErrInvalidArgument)
		})
	}
}

func sliceOf(m map[observation.GoroutineID]domain.GoroutineAtEnd) []domain.GoroutineAtEnd {
	out := make([]domain.GoroutineAtEnd, 0, len(m))
	for _, g := range m {
		out = append(out, g)
	}
	return out
}

func waitOn(ch chan struct{}) { <-ch }

func TestParseExecutionTrace_AcrossGenerations(t *testing.T) {
	ch := make(chan struct{})
	defer close(ch)
	var started time.Time
	raw := captureTrace(t, func() {
		started = time.Now()
		go waitOn(ch)
		// runtime は 1 秒ごとに世代を切り替え、そのたびに全 G の状態を書き直す
		time.Sleep(1200 * time.Millisecond)
	})
	tests := []struct {
		name string
	}{
		{name: "keeps when a goroutine started waiting even after the state is written again at a generation boundary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := domain.ParseExecutionTrace(raw, time.Now(), time.Time{})
			require.NoError(t, err)
			var found bool
			for _, g := range p.Goroutines {
				if g.Alive && observation.EntryFunction(g.Stack) == "github.com/kakkky/gorviz/internal/app/agent/domain_test.waitOn" {
					found = true
					assert.True(t, g.Determined)
					assert.Equal(t, "chan receive", g.Reason)
					assert.WithinDuration(t, started, g.Since, 100*time.Millisecond)
				}
			}
			assert.True(t, found)
		})
	}
}
