package domain_test

import (
	"testing"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/stretchr/testify/assert"
)

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
