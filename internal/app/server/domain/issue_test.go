package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/pkg/histogram"
)

func sched(d time.Duration, n int) histogram.Histogram {
	var h histogram.Histogram
	for range n {
		h.Observe(d)
	}
	return h
}

func TestJudgeChunk(t *testing.T) {
	t.Parallel()
	base := observation.TraceChunk{StartedAt: time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC), Duration: time.Second, Stats: observation.TraceStats{Procs: 4}}
	with := func(f func(c *observation.TraceChunk)) observation.TraceChunk {
		c := base
		f(&c)
		return c
	}
	tests := []struct {
		name  string
		chunk observation.TraceChunk
		want  map[domain.IssueKind]domain.Certainty
	}{
		{name: "finds nothing in a quiet chunk", chunk: base, want: map[domain.IssueKind]domain.Certainty{}},
		{
			name:  "judges cpu contention when the runnable p99 reaches 1ms",
			chunk: with(func(c *observation.TraceChunk) { c.Stats.SchedLatency = sched(2*time.Millisecond, 50) }),
			want:  map[domain.IssueKind]domain.Certainty{domain.IssueCPUContention: domain.CertaintyInference},
		},
		{
			name:  "does not judge cpu contention below 1ms",
			chunk: with(func(c *observation.TraceChunk) { c.Stats.SchedLatency = sched(200*time.Microsecond, 50) }),
			want:  map[domain.IssueKind]domain.Certainty{},
		},
		{
			name:  "judges gc pressure as a fact when gc takes half of the cpu",
			chunk: with(func(c *observation.TraceChunk) { c.Stats.ProcGC = 2 * time.Second }),
			want:  map[domain.IssueKind]domain.Certainty{domain.IssueGCPressure: domain.CertaintyFact},
		},
		{
			name: "judges gc pressure as an inference when mark assist takes over 5%",
			chunk: with(func(c *observation.TraceChunk) {
				c.GC = []observation.GCRange{{Kind: observation.GCMarkAssist, Goroutine: 3, Start: 0, End: 300 * time.Millisecond}}
				c.Procs = []observation.ProcLane{{Proc: 0, Spans: []observation.ProcSpan{{Goroutine: 3, Start: 0, End: 300 * time.Millisecond}}}}
			}),
			want: map[domain.IssueKind]domain.Certainty{domain.IssueGCPressure: domain.CertaintyInference},
		},
		{
			name: "judges lock contention when mutex waits exceed running time",
			chunk: with(func(c *observation.TraceChunk) {
				c.Stats.ProcRunning = 100 * time.Millisecond
				c.Stats.Waits = []observation.WaitSite{{Kind: observation.PlaceWaiting, Reason: "sync", Function: "sync.(*Mutex).Lock", Location: "a.go:1", Count: 3, Total: 500 * time.Millisecond}}
			}),
			want: map[domain.IssueKind]domain.Certainty{domain.IssueLockContention: domain.CertaintyInference},
		},
		{
			name: "does not count waitgroup waits as lock waits",
			chunk: with(func(c *observation.TraceChunk) {
				c.Stats.ProcRunning = 100 * time.Millisecond
				c.Stats.Waits = []observation.WaitSite{{Kind: observation.PlaceWaiting, Reason: "sync", Function: "sync.(*WaitGroup).Wait", Location: "a.go:1", Count: 3, Total: 500 * time.Millisecond}}
			}),
			want: map[domain.IssueKind]domain.Certainty{},
		},
		{
			name: "judges a long stw of 1ms or more",
			chunk: with(func(c *observation.TraceChunk) {
				c.GC = []observation.GCRange{{Kind: observation.GCStopTheWorld, Reason: "ReadMemStats", Start: 0, End: 2 * time.Millisecond}}
			}),
			want: map[domain.IssueKind]domain.Certainty{domain.IssueLongSTW: domain.CertaintyInference},
		},
		{
			name:  "judges thread explosion over half of the limit",
			chunk: with(func(c *observation.TraceChunk) { c.Threads = 5001 }),
			want:  map[domain.IssueKind]domain.Certainty{domain.IssueThreadExplosion: domain.CertaintyInference},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := map[domain.IssueKind]domain.Certainty{}
			for _, f := range domain.JudgeChunk("checkout", tt.chunk) {
				got[f.Kind] = f.Certainty
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModifiedZScore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		v      float64
		ref    []float64
		want   float64
		wantOK bool
	}{
		{name: "uses the median and the mad", v: 10, ref: []float64{1, 2, 3, 4, 5}, want: 0.6745 * (10 - 3) / 1, wantOK: true},
		{name: "falls back to the mean absolute deviation when the mad is zero", v: 5, ref: []float64{1, 1, 1, 1, 9}, want: 0.6745 * (5 - 1) / (1.253314 * 8.0 / 5), wantOK: true},
		{name: "does not judge when every reference is the same", v: 5, ref: []float64{2, 2, 2}, wantOK: false},
		{name: "does not judge without references", v: 5, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.ModifiedZScore(tt.v, tt.ref)
			require.Equal(t, tt.wantOK, ok)
			assert.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestJudgeBaseline(t *testing.T) {
	t.Parallel()
	sample := func(gc time.Duration) domain.MetricSample {
		return domain.MetricSample{Traced: time.Second, ProcCapacity: 10 * time.Second, GC: gc}
	}
	week := []domain.MetricSample{sample(100 * time.Millisecond), sample(110 * time.Millisecond), sample(90 * time.Millisecond), sample(105 * time.Millisecond)}
	tests := []struct {
		name      string
		current   domain.MetricSample
		reference [][]domain.MetricSample
		want      []domain.IssueKind
	}{
		{name: "judges a value far above the usual", current: sample(5 * time.Second), reference: [][]domain.MetricSample{week}, want: []domain.IssueKind{domain.IssueGCPressure}},
		{name: "does not judge a usual value", current: sample(100 * time.Millisecond), reference: [][]domain.MetricSample{week}},
		{name: "does not judge a value far below the usual", current: sample(0), reference: [][]domain.MetricSample{week}},
		{name: "does not judge until a week of history exists", current: sample(5 * time.Second), reference: [][]domain.MetricSample{nil, nil, nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []domain.IssueKind
			for _, f := range domain.JudgeBaseline("checkout", tt.current, tt.reference) {
				got = append(got, f.Kind)
				assert.Equal(t, domain.CertaintyInference, f.Certainty)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTracker(t *testing.T) {
	t.Parallel()
	inst := observation.Instance{Service: "checkout", Name: "a", Incarnation: "1"}
	t0 := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	finding := domain.Finding{Kind: domain.IssueCPUContention, Subject: "checkout", Certainty: domain.CertaintyInference, Judge: domain.JudgeByThreshold, Value: 1}
	tests := []struct {
		name        string
		steps       [][]domain.Finding
		wantStarted int
		wantOpen    int
		wantEnded   bool
	}{
		{name: "starts an occurrence once and keeps it while the condition holds", steps: [][]domain.Finding{{finding}, {finding}}, wantStarted: 1, wantOpen: 1},
		{name: "ends the occurrence when the condition stops holding", steps: [][]domain.Finding{{finding}, nil}, wantStarted: 1, wantOpen: 1, wantEnded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := domain.NewTracker(nil)
			for n, fs := range tt.steps {
				tr.Observe(t0.Add(time.Duration(n)*time.Second), inst, []domain.Judge{domain.JudgeByThreshold}, fs)
			}
			assert.Len(t, tr.Started(), tt.wantStarted)
			require.Len(t, tr.Open(), tt.wantOpen)
			assert.Equal(t, tt.wantEnded, !tr.Open()[0].Occurrence.Ongoing())
		})
	}
}

func TestShouldNotify(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	mine := domain.TrackedOccurrence{Issue: "X", Occurrence: domain.Occurrence{Instance: "a", Judge: domain.JudgeByThreshold, StartedAt: t0}}
	tests := []struct {
		name   string
		others []domain.TrackedOccurrence
		want   bool
	}{
		{name: "notifies when no instance has it ongoing", others: []domain.TrackedOccurrence{mine}, want: true},
		{name: "does not notify while another instance has it ongoing", others: []domain.TrackedOccurrence{mine, {Issue: "X", Occurrence: domain.Occurrence{Instance: "b", StartedAt: t0.Add(-time.Minute)}}}, want: false},
		{name: "notifies when another instance has already ended it", others: []domain.TrackedOccurrence{{Issue: "X", Occurrence: domain.Occurrence{Instance: "b", StartedAt: t0.Add(-time.Minute), EndedAt: t0}}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.ShouldNotify(mine, tt.others))
		})
	}
}
