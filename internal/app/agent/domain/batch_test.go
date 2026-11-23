package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestBuildBatch(t *testing.T) {
	t.Parallel()
	in := observation.Instance{Service: "s", Name: "i", Incarnation: "1"}
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	agent := observation.AgentID("agent-2")
	logs := []observation.LogLine{{Agent: agent, Seq: 1}, {Agent: agent, Seq: 2}, {Agent: agent, Seq: 3}}
	chunks := []observation.TraceChunk{{StartedAt: t0}, {StartedAt: t0.Add(5 * time.Second)}}
	snaps := []observation.GoroutineSnapshot{{At: t0.Add(5 * time.Second)}, {At: t0.Add(10 * time.Second)}}
	buf := domain.Buffered{Logs: logs, TraceChunks: chunks, Snapshots: snaps}
	tests := []struct {
		name  string
		after observation.Cursor
		buf   domain.Buffered
		ended bool
		want  observation.Batch
	}{
		{
			name: "sends everything when the server has nothing",
			buf:  buf,
			want: observation.Batch{Instance: in, Logs: logs, TraceChunks: chunks, Snapshots: snaps},
		},
		{
			name:  "sends what follows the server's cursor",
			after: observation.Cursor{LogAgent: agent, LogSeq: 2, TraceChunk: t0, Snapshot: t0.Add(5 * time.Second)},
			buf:   buf,
			want: observation.Batch{
				Instance: in, After: observation.Cursor{LogAgent: agent, LogSeq: 2, TraceChunk: t0, Snapshot: t0.Add(5 * time.Second)},
				Logs: logs[2:], TraceChunks: chunks[1:], Snapshots: snaps[1:],
			},
		},
		{
			name:  "sends every log of this agent when the server's log cursor is of another agent start",
			after: observation.Cursor{LogAgent: "agent-1", LogSeq: 100},
			buf:   domain.Buffered{Logs: logs},
			want:  observation.Batch{Instance: in, After: observation.Cursor{LogAgent: "agent-1", LogSeq: 100}, Logs: logs},
		},
		{
			name:  "sends from the oldest it still has when the server's cursor is older than that",
			after: observation.Cursor{TraceChunk: t0.Add(-time.Hour)},
			buf:   domain.Buffered{TraceChunks: chunks[1:]},
			want:  observation.Batch{Instance: in, After: observation.Cursor{TraceChunk: t0.Add(-time.Hour)}, TraceChunks: chunks[1:]},
		},
		{
			name:  "tells the end of the instance even with nothing left",
			ended: true,
			want:  observation.Batch{Instance: in, Ended: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.BuildBatch(in, tt.after, agent, tt.buf, tt.ended))
		})
	}
}
