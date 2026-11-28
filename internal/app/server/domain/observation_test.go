package domain_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/trace"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// observationsOf は testdata の trace (4 つの goroutine が 1 つの Mutex を 20ms ずつ取り合い、そのあと 3 つの goroutine が channel の送信でリークする) を、
// 1 秒ごとの Observation と、セッション全体の要約にする。leakAt は goroutineleak プロファイルを取った時刻で、trace に書いた "leak-check 1" のログから取る
func observationsOf(t *testing.T) ([]domain.Observation, domain.Observed, time.Duration) {
	t.Helper()
	data, err := os.ReadFile("testdata/lock_and_leak.trace")
	require.NoError(t, err)
	events, err := domain.DecodeTraceChunk(data)
	require.NoError(t, err)
	b := domain.NewObservationBuilder()
	obs := b.ApplyChunk(events, 0)
	last, until := b.End()
	obs = append(obs, last)
	var leakAt time.Duration
	for _, ev := range events {
		if ev.Kind() == trace.EventLog && ev.Log().Message == "leak-check 1" {
			leakAt = ev.Time().Sub(events[0].Time())
		}
	}
	require.NotZero(t, leakAt)
	return obs, domain.Observed{Started: true, Until: until, Ended: true}, leakAt
}

func setOf(t *testing.T) (*domain.ObservationSet, time.Duration) {
	t.Helper()
	obs, observed, leakAt := observationsOf(t)
	return domain.NewObservationSet(obs, observed), leakAt
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
	ix, leakAt := setOf(t)
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
			name: "matches every leaked goroutine of the profile as a fact and groups them by the go statement",
			check: func(t *testing.T) {
				at := leakAt
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
				at := leakAt
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

// chunksOf は testdata の 2 つの trace の区切り (1 つ目と 2 つ目の間に ends が終わり、wakes が起きて走り出し、stays は待ち続ける) を足し、
// 2 つ目の区切りの頭の時刻を返す
func chunksOf(t *testing.T) (*domain.ObservationSet, time.Duration) {
	t.Helper()
	b := domain.NewObservationBuilder()
	var obs []domain.Observation
	var first trace.Time
	var second time.Duration
	for i, name := range []string{"testdata/two_chunks_1.trace", "testdata/two_chunks_2.trace"} {
		data, err := os.ReadFile(name)
		require.NoError(t, err)
		events, err := domain.DecodeTraceChunk(data)
		require.NoError(t, err)
		if i == 0 {
			first = events[0].Time()
		} else {
			second = events[0].Time().Sub(first)
		}
		obs = append(obs, b.ApplyChunk(events, 4)...)
	}
	last, until := b.End()
	obs = append(obs, last)
	return domain.NewObservationSet(obs, domain.Observed{Started: true, Until: until, Ended: true}), second
}

func goroutineOf(t *testing.T, ix *domain.ObservationSet, entry string) *domain.GoroutineRecord {
	t.Helper()
	for _, id := range ix.GoroutineIDs() {
		if g, _ := ix.Goroutine(id); g.Entry == entry {
			return g
		}
	}
	t.Fatalf("no goroutine of %s", entry)
	return nil
}

func TestObservationBuilderChunks(t *testing.T) {
	t.Parallel()
	ix, second := chunksOf(t)
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "ends a goroutine missing from the next chunk at the head of that chunk",
			check: func(t *testing.T) {
				g := goroutineOf(t, ix, "main.ends")
				assert.True(t, g.Ended)
				assert.Equal(t, second, g.EndedAt)
			},
		},
		{
			name: "keeps a goroutine waiting across the chunks without a change at the seam",
			check: func(t *testing.T) {
				g := goroutineOf(t, ix, "main.stays")
				assert.False(t, g.Ended)
				c, ok := ix.StateAt(g, ix.Last())
				require.True(t, ok)
				assert.Equal(t, domain.PlaceWaiting, c.Place)
				assert.Less(t, c.At, second)
			},
		},
		{
			name: "moves a goroutine woken between the chunks out of waiting at the head of the next chunk",
			check: func(t *testing.T) {
				g := goroutineOf(t, ix, "main.wakes")
				c, ok := ix.StateAt(g, second)
				require.True(t, ok)
				assert.NotEqual(t, domain.PlaceWaiting, c.Place)
			},
		},
		{
			name: "takes the number of threads from the chunk",
			check: func(t *testing.T) {
				bs := ix.Buckets(0, ix.Last())
				require.NotEmpty(t, bs)
				assert.Equal(t, 4, bs[len(bs)-1].Threads)
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

func TestParseThreadCreateProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "reads the total of the threadcreate profile", text: "threadcreate profile: total 12\n1 @\n#\t0x1\n", want: 12},
		{name: "reads nothing from an unknown text", text: "not a profile\n", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.ParseThreadCreateProfile([]byte(tt.text)))
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
