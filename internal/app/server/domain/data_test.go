package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestFileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{name: "replaces characters unusable in a file name", parts: []string{"GOROUTINE_LEAK", "main.(*Server).handle"}, want: "GOROUTINE_LEAK--main-Server-handle--"},
		{name: "keeps an empty part readable", parts: []string{"LONG_STW", ""}, want: "LONG_STW-----"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.FileName(tt.parts...)
			assert.Contains(t, got, tt.want)
			assert.Equal(t, got, domain.FileName(tt.parts...))
		})
	}
	long := strings.Repeat("y", 100)
	a, b := domain.FileName("K", long+"1"), domain.FileName("K", long+"2")
	assert.NotEqual(t, a, b, "truncated names stay apart by the hash")
}

func TestMessageShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{name: "folds numbers", msg: "request failed user_id=123", want: "request failed user_id=n"},
		{name: "folds quoted values and ids", msg: `lookup "alice" took 12.5ms id=0xdeadbeef`, want: "lookup s took nms id=n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.MessageShape(tt.msg))
		})
	}
}

func TestNormalizeFunction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fn   string
		want string
	}{
		{name: "folds closures into the outer function", fn: "main.(*Server).handle.func1.2", want: "main.(*Server).handle"},
		{name: "folds type parameters", fn: "main.Map[go.shape.int].func3", want: "main.Map[...]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.NormalizeFunction(tt.fn))
		})
	}
}

func TestRetentionExpired(t *testing.T) {
	t.Parallel()
	r := domain.Retention{ExecutionTraces: 7 * 24 * time.Hour, Metrics: 31 * 24 * time.Hour}
	now := time.Date(2026, 11, 21, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		name  string
		kind  domain.DataKind
		start time.Time
		want  bool
	}{
		{name: "keeps an hourly file whose end is inside the period", kind: domain.DataExecutionTraces, start: time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC), want: false},
		{name: "drops an hourly file whose end passed the period", kind: domain.DataExecutionTraces, start: time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC), want: true},
		{name: "keeps a daily file that still has data inside the period", kind: domain.DataMetrics, start: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC), want: false},
		{name: "drops a daily file that ended before the period", kind: domain.DataMetrics, start: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, r.Expired(tt.kind, tt.start, now))
		})
	}
}

func TestOverCapacity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	files := []domain.DataFile{
		{Kind: domain.DataMetrics, Path: "m19", PeriodStart: day(19), Size: 10},
		{Kind: domain.DataMetrics, Path: "m20", PeriodStart: day(20), Size: 10},
		{Kind: domain.DataMetrics, Path: "m21", PeriodStart: day(21), Size: 10},
		{Kind: domain.DataLogs, Path: "l20", PeriodStart: day(20).Add(3 * time.Hour), Size: 10},
		{Kind: domain.DataLogs, Path: "l21", PeriodStart: day(21).Add(3 * time.Hour), Size: 10},
	}
	tests := []struct {
		name  string
		limit int64
		want  []string
	}{
		{name: "drops nothing under the limit", limit: 100},
		{name: "drops the oldest day of every kind", limit: 45, want: []string{"l20", "m19"}},
		{name: "repeats a day at a time but never drops today", limit: 1, want: []string{"l20", "m19", "m20"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, f := range domain.OverCapacity(files, tt.limit, now) {
				got = append(got, f.Path)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPlanIngest(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	r := domain.Retention{Logs: 24 * time.Hour, ExecutionTraces: 24 * time.Hour, GoroutineSnapshots: 24 * time.Hour, GoroutineLeakChecks: 24 * time.Hour}
	chunk := func(at time.Time) observation.TraceChunk {
		return observation.TraceChunk{StartedAt: at, Duration: time.Second}
	}
	held := observation.Cursor{LogAgent: "a1", LogSeq: 5, TraceChunk: now.Add(-time.Minute)}
	tests := []struct {
		name       string
		batch      observation.Batch
		wantLogs   []uint64
		wantChunks int
	}{
		{name: "drops what is already held and writes the rest",
			batch:    observation.Batch{After: observation.Cursor{LogAgent: "a1", LogSeq: 3, TraceChunk: now.Add(-2 * time.Minute)}, Logs: []observation.LogLine{{Agent: "a1", Seq: 4, At: now}, {Agent: "a1", Seq: 6, At: now}}, TraceChunks: []observation.TraceChunk{chunk(now.Add(-time.Minute)), chunk(now)}},
			wantLogs: []uint64{6}, wantChunks: 1},
		{name: "writes nothing of a kind whose position is ahead of what is held",
			batch: observation.Batch{After: observation.Cursor{LogAgent: "a1", LogSeq: 9, TraceChunk: now}, Logs: []observation.LogLine{{Agent: "a1", Seq: 10, At: now}}, TraceChunks: []observation.TraceChunk{chunk(now.Add(time.Second))}}},
		{name: "writes logs of a new agent from its first line",
			batch:    observation.Batch{Logs: []observation.LogLine{{Agent: "a2", Seq: 1, At: now}}},
			wantLogs: []uint64{1}},
		{name: "drops late data past the retention",
			batch: observation.Batch{Logs: []observation.LogLine{{Agent: "a1", Seq: 7, At: now.Add(-48 * time.Hour)}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := domain.PlanIngest(tt.batch, held, r, now)
			var seqs []uint64
			for _, l := range p.Logs {
				seqs = append(seqs, l.Seq)
			}
			assert.Equal(t, tt.wantLogs, seqs)
			assert.Len(t, p.TraceChunks, tt.wantChunks)
		})
	}
}

func TestLifecycleChanges(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	inst := func(inc observation.Incarnation) observation.Instance {
		return observation.Instance{Service: "s", Name: "a", Incarnation: inc}
	}
	tests := []struct {
		name    string
		last    domain.LifecycleEvent
		hasLast bool
		batch   observation.Batch
		want    []domain.LifecycleKind
		restart bool
	}{
		{name: "starts a new instance", batch: observation.Batch{Instance: inst("1")}, want: []domain.LifecycleKind{domain.LifecycleStart}},
		{name: "records nothing while the same incarnation continues", last: domain.LifecycleEvent{Instance: "a", Incarnation: "1", Kind: domain.LifecycleStart}, hasLast: true, batch: observation.Batch{Instance: inst("1")}},
		{name: "ends the previous and starts the new on a restart with the same name", last: domain.LifecycleEvent{Instance: "a", Incarnation: "1", Kind: domain.LifecycleStart}, hasLast: true,
			batch: observation.Batch{Instance: inst("2")}, want: []domain.LifecycleKind{domain.LifecycleEnd, domain.LifecycleStart}, restart: true},
		{name: "ends the instance on the last batch", last: domain.LifecycleEvent{Instance: "a", Incarnation: "1", Kind: domain.LifecycleStart}, hasLast: true,
			batch: observation.Batch{Instance: inst("1"), Ended: true}, want: []domain.LifecycleKind{domain.LifecycleEnd}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.LifecycleChanges(tt.last, tt.hasLast, tt.batch, now)
			var kinds []domain.LifecycleKind
			for _, e := range got {
				kinds = append(kinds, e.Kind)
				if e.Kind == domain.LifecycleStart {
					assert.Equal(t, tt.restart, e.Restart)
				}
			}
			assert.Equal(t, tt.want, kinds)
		})
	}
}

func TestUncovered(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	tests := []struct {
		name     string
		existing []domain.PreservedTrace
		wantFrom time.Time
		wantTo   time.Time
		wantOK   bool
	}{
		{name: "keeps the whole range without overlap", wantFrom: at(0), wantTo: at(10), wantOK: true},
		{name: "keeps only the part after what was kept", existing: []domain.PreservedTrace{{From: at(-5), To: at(4)}}, wantFrom: at(4), wantTo: at(10), wantOK: true},
		{name: "keeps the longer side around what was kept in the middle", existing: []domain.PreservedTrace{{From: at(2), To: at(4)}}, wantFrom: at(4), wantTo: at(10), wantOK: true},
		{name: "keeps nothing when it was all kept", existing: []domain.PreservedTrace{{From: at(-1), To: at(11)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, to, ok := domain.Uncovered(at(0), at(10), tt.existing)
			require.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantFrom, f)
				assert.Equal(t, tt.wantTo, to)
			}
		})
	}
}
