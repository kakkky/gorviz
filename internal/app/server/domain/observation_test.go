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

// observationsOf は testdata の trace (4 つの goroutine が 1 つの Mutex を 20ms ずつ取り合い、そのあと 3 つの goroutine が channel の送信でリークする) を、
// 1 秒ごとの Observation と、セッション全体の要約にする
func observationsOf(t *testing.T) ([]domain.Observation, domain.Observed) {
	t.Helper()
	f, err := os.Open("testdata/lock_and_leak.trace")
	require.NoError(t, err)
	defer f.Close()
	dec, err := domain.NewTraceDecoder(f)
	require.NoError(t, err)
	b := domain.NewObservationBuilder()
	var obs []domain.Observation
	for {
		ev, err := dec.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		obs = append(obs, b.Apply(ev)...)
	}
	last, until := b.End()
	obs = append(obs, last)
	observed := domain.Observed{Started: true, Until: until, Ended: true, LeakChecks: map[uint64]time.Duration{}}
	marks, checks := b.TakeNews()
	observed.Marks = marks
	for seq, at := range checks {
		observed.LeakChecks[seq] = at
	}
	return obs, observed
}

func setOf(t *testing.T) (*domain.ObservationSet, domain.Observed) {
	t.Helper()
	obs, observed := observationsOf(t)
	return domain.NewObservationSet(obs, observed), observed
}

func leakStacks(t *testing.T) []domain.LeakProfileStack {
	t.Helper()
	b, err := os.ReadFile("testdata/lock_and_leak.leak")
	require.NoError(t, err)
	stacks, err := domain.ParseGoroutineLeakProfile(b)
	require.NoError(t, err)
	return stacks
}

func TestObservationBuilder(t *testing.T) {
	t.Parallel()
	ix, observed := setOf(t)
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
				_, ok := observed.LeakCheckAt(1)
				assert.True(t, ok)
			},
		},
		{
			name: "matches every leaked goroutine of the profile as a fact and groups them by the go statement",
			check: func(t *testing.T) {
				at, _ := observed.LeakCheckAt(1)
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
				at, _ := observed.LeakCheckAt(1)
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

func TestObservedTraceAt(t *testing.T) {
	t.Parallel()
	wall := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		clock  domain.ClockSync
		wall   time.Time
		want   time.Duration
		wantOK bool
	}{
		{name: "puts a wall time on the trace from the clock of the trace", clock: domain.ClockSync{At: 3 * time.Second, Wall: wall}, wall: wall.Add(1500 * time.Millisecond), want: 4500 * time.Millisecond, wantOK: true},
		{name: "puts a time before the trace at its start", clock: domain.ClockSync{At: time.Second, Wall: wall}, wall: wall.Add(-2 * time.Second), want: 0, wantOK: true},
		{name: "cannot put a time before the clock of the trace arrives", wall: wall},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.Observed{Clock: tt.clock}.TraceAt(tt.wall)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestObservedAddMarks(t *testing.T) {
	t.Parallel()
	before := []domain.TraceMark{{At: time.Second, Message: "a"}, {At: 3 * time.Second, Message: "c"}}
	o := domain.Observed{Marks: before}
	o.AddMarks(domain.TraceMark{At: 2 * time.Second, Message: "b"})
	assert.Equal(t, []domain.TraceMark{{At: time.Second, Message: "a"}, {At: 2 * time.Second, Message: "b"}, {At: 3 * time.Second, Message: "c"}}, o.Marks)
	assert.Len(t, before, 2)
}
