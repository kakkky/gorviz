package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestChunkSample_MarkAssist(t *testing.T) {
	t.Parallel()
	ms := time.Millisecond
	tests := []struct {
		name  string
		chunk observation.TraceChunk
		want  time.Duration
	}{
		{
			name: "counts mark assist only while the goroutine runs on a P",
			chunk: observation.TraceChunk{
				Duration: 100 * ms,
				Stats:    observation.TraceStats{Procs: 1},
				Procs:    []observation.ProcLane{{Proc: 0, Spans: []observation.ProcSpan{{Goroutine: 1, Start: 0, End: 10 * ms}, {Goroutine: 2, Start: 10 * ms, End: 20 * ms}}}},
				GC: []observation.GCRange{
					{Kind: observation.GCMarkAssist, Goroutine: 1, Start: 5 * ms, End: 90 * ms},
					{Kind: observation.GCMarkAssist, Goroutine: 2, Start: 0, End: 90 * ms},
				},
			},
			want: 15 * ms,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := domain.ChunkSample(tt.chunk)
			assert.Equal(t, tt.want, s.MarkAssist)
			assert.LessOrEqual(t, s.MarkAssistShare(), 1.0)
		})
	}
}
