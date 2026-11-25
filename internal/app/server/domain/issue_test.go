package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestIssueBook(t *testing.T) {
	t.Parallel()
	lock := func(from, to time.Duration) domain.Detection {
		return domain.Detection{Kind: domain.IssueLockContention, From: from, To: to, Certainty: domain.CertaintyInference}
	}
	tests := []struct {
		name  string
		run   func(b *domain.IssueBook)
		check func(t *testing.T, b *domain.IssueBook)
	}{
		{
			name: "extends an occurrence that continues without a gap",
			run: func(b *domain.IssueBook) {
				b.Record(lock(0, time.Second), "")
				b.Record(lock(time.Second, 2*time.Second), "smoke")
			},
			check: func(t *testing.T, b *domain.IssueBook) {
				require.Len(t, b.Issues(), 1)
				require.Len(t, b.Issues()[0].Occurrences, 1)
				assert.Equal(t, 2*time.Second, b.Issues()[0].Occurrences[0].To)
				assert.Equal(t, "smoke", b.Issues()[0].FirstPhase())
			},
		},
		{
			name: "starts a new occurrence after a gap and keeps one issue per kind and target",
			run: func(b *domain.IssueBook) {
				b.Record(lock(0, time.Second), "")
				b.Record(lock(5*time.Second, 6*time.Second), "")
				b.Record(domain.Detection{Kind: domain.IssueLongSTW, Target: "GC mark termination", From: 0, To: time.Second}, "")
			},
			check: func(t *testing.T, b *domain.IssueBook) {
				require.Len(t, b.Issues(), 2)
				assert.Len(t, b.Issues()[0].Occurrences, 2)
				assert.Equal(t, domain.IssueID("I2"), b.Issues()[1].ID)
			},
		},
		{
			name: "tags the phase on an occurrence that the phase judgment overlaps",
			run: func(b *domain.IssueBook) {
				b.Record(lock(2*time.Second, 3*time.Second), "")
				b.RecordPhase(lock(time.Second, 4*time.Second), "ramp-up 2")
			},
			check: func(t *testing.T, b *domain.IssueBook) {
				require.Len(t, b.Issues()[0].Occurrences, 1)
				assert.Equal(t, "ramp-up 2", b.Issues()[0].Occurrences[0].Phase)
			},
		},
		{
			name: "answers a missing issue with the issues that exist",
			run: func(b *domain.IssueBook) {
				b.Record(lock(0, time.Second), "")
			},
			check: func(t *testing.T, b *domain.IssueBook) {
				err := domain.IssueNotFound("I9", b.Issues())
				require.ErrorIs(t, err, domain.ErrNotFound)
				assert.Contains(t, err.Error(), "I1")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := domain.NewIssueBook()
			tt.run(b)
			tt.check(t, b)
		})
	}
}

func TestDetectRuntimeIssues(t *testing.T) {
	t.Parallel()
	slow := domain.Histogram{}
	for range 100 {
		slow.Add(5 * time.Millisecond)
	}
	tests := []struct {
		name string
		w    domain.WindowStats
		want map[domain.IssueKind]domain.Certainty
	}{
		{
			name: "finds nothing in a quiet second",
			w:    domain.WindowStats{From: 0, To: time.Second, Procs: 4, Running: 100 * time.Millisecond},
			want: map[domain.IssueKind]domain.Certainty{},
		},
		{
			name: "finds CPU contention when p99 of the wait for a P reaches 1ms",
			w:    domain.WindowStats{From: 0, To: time.Second, Procs: 4, RunnableWait: slow},
			want: map[domain.IssueKind]domain.Certainty{domain.IssueCPUContention: domain.CertaintyInference},
		},
		{
			name: "finds GC pressure as a fact when GC used half of the CPU",
			w:    domain.WindowStats{From: 0, To: time.Second, Procs: 2, GCWorker: time.Second},
			want: map[domain.IssueKind]domain.Certainty{domain.IssueGCPressure: domain.CertaintyFact},
		},
		{
			name: "finds lock contention when goroutines waited on locks longer than they ran",
			w:    domain.WindowStats{From: 0, To: time.Second, Procs: 4, LockWait: time.Second, Running: 200 * time.Millisecond},
			want: map[domain.IssueKind]domain.Certainty{domain.IssueLockContention: domain.CertaintyInference},
		},
		{
			name: "finds a long stop-the-world and too many threads",
			w: domain.WindowStats{From: 0, To: time.Second, Procs: 4, MaxThreads: 6000,
				STWs: []domain.TimeRange{{Start: 0, End: 2 * time.Millisecond, Reason: "GC mark termination"}}},
			want: map[domain.IssueKind]domain.Certainty{domain.IssueLongSTW: domain.CertaintyInference, domain.IssueThreadExplosion: domain.CertaintyInference},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := map[domain.IssueKind]domain.Certainty{}
			for _, d := range domain.DetectRuntimeIssues(tt.w) {
				got[d.Kind] = d.Certainty
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHistogramQuantile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []time.Duration
		q      float64
		lo, hi time.Duration
	}{
		{name: "returns 0 for no values", q: 0.99},
		{name: "answers within an eighth of the value", values: []time.Duration{time.Millisecond}, q: 0.5, lo: time.Millisecond, hi: time.Millisecond * 9 / 8},
		{name: "puts p99 on the slow tail", values: append(make([]time.Duration, 98), 10*time.Millisecond, 10*time.Millisecond), q: 0.99, lo: 10 * time.Millisecond, hi: 12 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var h domain.Histogram
			for _, v := range tt.values {
				h.Add(v)
			}
			got := h.Quantile(tt.q)
			assert.GreaterOrEqual(t, got, tt.lo)
			assert.LessOrEqual(t, got, tt.hi)
		})
	}
}
