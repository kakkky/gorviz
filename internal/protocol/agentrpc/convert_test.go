package agentrpc_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/pkg/histogram"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
)

func TestExportRequest_RoundTrip(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 21, 14, 3, 12, 0, time.UTC)
	var h histogram.Histogram
	h.Observe(3 * time.Millisecond)
	tests := []struct {
		name  string
		batch observation.Batch
	}{
		{
			name: "keeps every observation of a batch",
			batch: observation.Batch{
				Instance: observation.Instance{Service: "checkout", Name: "checkout-7d9f", Incarnation: "c1"},
				After:    observation.Cursor{LogAgent: "a1", LogSeq: 4, TraceChunk: at, Snapshot: at, LeakCheck: at},
				Logs:     []observation.LogLine{{Agent: "a1", Seq: 5, At: at, TimeFromLog: true, Level: observation.LogLevelError, Message: "failed", Raw: `{"msg":"failed"}`, Source: observation.LogSourceStdin, TraceSeq: 9, HasTraceSeq: true, Duration: time.Second, HasDuration: true}},
				TraceChunks: []observation.TraceChunk{{
					StartedAt: at, Duration: 5 * time.Second,
					Events:   []observation.StateChange{{At: time.Millisecond, Kind: observation.StateChangeGoroutine, Goroutine: 7, Place: observation.Place{Kind: observation.PlaceWaiting, Reason: "sync"}, Function: "sync.(*Mutex).Lock", Location: "main.go:10", WokenBy: 3}},
					Procs:    []observation.ProcLane{{Proc: 0, Spans: []observation.ProcSpan{{Goroutine: 7, Thread: 1, Start: 0, End: time.Millisecond}}}},
					Entries:  map[observation.GoroutineID]string{7: "main.worker"},
					GC:       []observation.GCRange{{Kind: observation.GCStopTheWorld, Reason: "GC sweep termination", Start: 1, End: 2}},
					Heap:     []observation.HeapSample{{At: 1, ObjectsBytes: 10, GoalBytes: 20}},
					GCEnds:   []observation.GCEnd{{At: 2, LiveHeapBytes: 5, HeapGoalBytes: 10}},
					Threads:  12,
					LogMarks: []observation.LogMark{{Seq: 9, Goroutine: 7, At: 3}},
					Stats:    observation.TraceStats{Procs: 4, Runnable: time.Second, SchedLatency: h, STW: h, GCMaxShare: 0.5, Waits: []observation.WaitSite{{Kind: observation.PlaceWaiting, Reason: "sync", Function: "sync.(*Mutex).Lock", Count: 1, Total: time.Second, Max: time.Second}}},
				}},
				Snapshots: []observation.GoroutineSnapshot{{At: at, Goroutines: []observation.Goroutine{{
					ID: 7, State: observation.GoroutineWaiting, WaitReason: "chan receive", Waiting: time.Minute, WaitingLowerBound: true, Entry: "main.worker",
					Current: observation.Frame{Function: "main.worker", File: "main.go", Line: 3}, CreatedBy: observation.Frame{Function: "main.main", File: "main.go", Line: 1},
					Stack: []observation.Frame{{Function: "runtime.gopark", File: "proc.go", Line: 1}},
				}}}},
				LeakChecks: []observation.GoroutineLeakCheck{{At: at, Status: observation.LeakCheckDone, Leaks: []observation.LeakedStack{{Stack: []observation.Frame{{Function: "main.worker", File: "main.go", Line: 3}}, Count: 2}}}},
				Ended:      true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := proto.Marshal(agentrpc.ToExportRequest([]observation.Batch{tt.batch}))
			assert.NoError(t, err)
			var req agentrpc.ExportRequest
			assert.NoError(t, proto.Unmarshal(b, &req))
			got := agentrpc.FromExportRequest(&req)
			assert.Equal(t, []observation.Batch{tt.batch}, got)
		})
	}
}
