package domain_test

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// indexOf は testdata の trace (4 つの goroutine が 1 つの Mutex を 20ms ずつ取り合い、そのあと 3 つの goroutine が channel の送信でリークする) を index にする
func indexOf(t *testing.T) *domain.TraceIndex {
	t.Helper()
	f, err := os.Open("testdata/lock_and_leak.trace")
	require.NoError(t, err)
	defer f.Close()
	dec, err := domain.NewTraceDecoder(f)
	require.NoError(t, err)
	ix := domain.NewTraceIndex()
	for {
		ev, err := dec.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		ix.Apply(ev)
	}
	ix.End(false)
	return ix
}

func leakStacks(t *testing.T) []domain.LeakProfileStack {
	t.Helper()
	b, err := os.ReadFile("testdata/lock_and_leak.leak")
	require.NoError(t, err)
	stacks, err := domain.ParseGoroutineLeakProfile(b)
	require.NoError(t, err)
	return stacks
}

func TestTraceIndex(t *testing.T) {
	t.Parallel()
	ix := indexOf(t)
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "counts the lock wait of the goroutines that took turns on the mutex",
			check: func(t *testing.T) {
				w := domain.NewWindowStats(ix, 0, ix.Last())
				assert.Greater(t, w.LockWait, 100*time.Millisecond)
			},
		},
		{
			name: "finds the trace mark written for the leak check",
			check: func(t *testing.T) {
				_, ok := ix.LeakCheckAt(1)
				assert.True(t, ok)
			},
		},
		{
			name: "matches every leaked goroutine of the profile as a fact and groups them by the go statement",
			check: func(t *testing.T) {
				at, _ := ix.LeakCheckAt(1)
				ds := domain.MatchLeaks(ix, at, leakStacks(t))
				require.Len(t, ds, 1)
				assert.Equal(t, domain.IssueGoroutineLeak, ds[0].Kind)
				assert.Equal(t, "main.leak", ds[0].Target)
				require.Len(t, ds[0].Leaks, 1)
				l := ds[0].Leaks[0]
				assert.Equal(t, 3, l.Count)
				assert.Equal(t, 3, l.Of)
				assert.Len(t, l.Goroutines, 3)
				assert.Equal(t, domain.CertaintyFact, l.Certainty)
				assert.Equal(t, "chan send", l.Reason)
			},
		},
		{
			name: "says a leaked goroutine waited on a channel at the send",
			check: func(t *testing.T) {
				at, _ := ix.LeakCheckAt(1)
				ds := domain.MatchLeaks(ix, at, leakStacks(t))
				g := ds[0].Leaks[0].Goroutines[0]
				d, err := domain.DetailOf(ix, g, 0, at, ds[0].Leaks)
				require.NoError(t, err)
				assert.Equal(t, domain.CauseSyncWait, d.MainCause)
				assert.Equal(t, "main.leak.func1", d.StoppedAt.Function)
				assert.Equal(t, 23, d.StoppedAt.Line)
				assert.Equal(t, "main.leak", d.Creator.Function)
				require.NotNil(t, d.Leak)
				assert.Equal(t, domain.CertaintyFact, d.Leak.Certainty)
			},
		},
		{
			name: "returns nearby goroutines when one is not found",
			check: func(t *testing.T) {
				_, err := domain.DetailOf(ix, 1<<40, 0, ix.Last(), nil)
				require.ErrorIs(t, err, domain.ErrNotFound)
			},
		},
		{
			name: "builds a scheduler window with the goroutines alive at its start",
			check: func(t *testing.T) {
				w := domain.NewSchedulerWindow(ix, ix.Last()/2, ix.Last())
				assert.NotEmpty(t, w.Changes)
				assert.NotEmpty(t, w.Procs)
				assert.True(t, w.Ended)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t)
		})
	}
}
