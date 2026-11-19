package agentrpc_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/kakkky/gorviz/internal/app/api/agentrpc"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

func roundTrip[T proto.Message](t *testing.T, m T, empty T) T {
	t.Helper()
	b, err := proto.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, proto.Unmarshal(b, empty))
	return empty
}

func TestTraceChunkRoundTrip(t *testing.T) {
	t.Parallel()
	var h model.Histogram
	h.Observe(3 * time.Microsecond)
	h.Observe(2 * time.Millisecond)
	chunk := model.TraceChunk{
		StartedAt: time.Date(2026, 11, 20, 12, 0, 0, 123, time.Local),
		Timeline: model.SchedulerTimeline{
			Window:              time.Second,
			Procs:               []model.ProcLane{{Proc: 1, Spans: []model.SchedSpan{{Goroutine: 7, Thread: 2, Start: 10, End: 20}}}},
			Threads:             []model.ThreadID{2, 3},
			GoroutinesCreated:   4,
			GoroutinesDestroyed: 1,
			Events: []model.SchedEvent{
				{
					At: 5, Kind: model.SchedEventGoroutine, Goroutine: 7,
					Place:    model.GoroutinePlace{Kind: model.PlaceSyscall, Proc: 1, HasProc: true, Thread: 2, Reason: "chan receive", Cgo: true},
					Function: "sync.(*Mutex).Lock", Location: "main.go:10", Created: true,
					WokenBy: 8, WokenByFunction: "sync.(*Mutex).Unlock", WokenByLocation: "main.go:20",
				},
				{At: 6, Kind: model.SchedEventProc, Proc: 1, ProcStatus: model.ProcRunning, Thread: 3},
			},
			EventsTruncated: true,
			Entries:         map[model.GoroutineID]string{7: "main.worker"},
			GC:              []model.GCRange{{Kind: model.GCStopTheWorld, Goroutine: 7, Reason: "GC sweep termination", Start: 1, End: 2}},
			Heap:            []model.HeapSample{{At: 3, ObjectsBytes: 100, GoalBytes: 200}},
			GCCycles:        2,
			GCEnds:          []model.GCEnd{{At: 4, LiveHeapBytes: 50, HeapGoalBytes: 100}},
		},
		LogMarks: []model.TraceLogMark{{Seq: 3, Goroutine: 9, At: 7}},
		Stats: model.TraceStats{
			Window: time.Second, Procs: 8, Runnable: 3 * time.Millisecond,
			Waits: []model.WaitSite{{
				Kind: model.PlaceWaiting, Reason: "sync.Mutex.Lock", Cgo: true, Entry: "main.worker", Function: "sync.(*Mutex).Lock",
				Location: "main.go:10", Count: 2, Total: time.Millisecond, Max: 600 * time.Microsecond,
			}},
			SchedLatency: h, STW: h, GCMaxShare: 0.25, ProcRunning: 5 * time.Millisecond, ProcGC: time.Millisecond,
		},
		Raw: []byte("raw"),
	}

	got := roundTrip(t, agentrpc.NewTraceChunk(chunk), &agentrpc.TraceChunk{}).Model()

	assert.Equal(t, chunk, got, "every field of a chunk survives the wire")
}

func TestLogLineRoundTrip(t *testing.T) {
	t.Parallel()
	lines := []model.LogLine{
		{
			ID: 1, At: time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local), TimeFromLog: true, Level: model.LogLevelWarn, Message: "slow",
			Raw: `{"msg":"slow"}`, Source: "stdout", TraceSeq: 0, HasTraceSeq: true, Duration: 0, HasDuration: true,
		},
		{ID: 2, Message: "no time, no seq"},
	}

	var got []model.LogLine
	for _, l := range agentrpc.NewLogLines(lines) {
		got = append(got, roundTrip(t, l, &agentrpc.LogLine{}).Model())
	}

	assert.Equal(t, lines, got, "a zero trace seq or duration that was present stays present, and a missing time stays zero")
}

func TestGoroutineDumpRoundTrip(t *testing.T) {
	t.Parallel()
	dump := model.GoroutineDump{
		At:        time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local),
		LeakCheck: model.LeakCheckDone,
		Goroutines: []model.Goroutine{{
			ID: 1, State: "chan receive", Wait: time.Minute, EntryFunction: "main.worker", CurrentFunction: "main.wait",
			CreatedBy: "main.main", CreatedAt: "main.go:5", Leaked: true, Labels: map[string]string{"scope": "s"},
		}},
	}

	got := roundTrip(t, agentrpc.NewGoroutineDump(dump), &agentrpc.GoroutineDump{}).Model()

	assert.Equal(t, dump, got)
}

func TestRecordingSpanRoundTrip(t *testing.T) {
	t.Parallel()
	span := model.RecordingSpan{From: time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local), To: time.Date(2026, 11, 20, 12, 1, 0, 0, time.Local), Bytes: 10, LimitBytes: 20}

	got := roundTrip(t, agentrpc.NewRecordingSpan(span), &agentrpc.RecordingSpan{}).Model()

	assert.Equal(t, span, got)
}

func TestMissingMessageIsZero(t *testing.T) {
	t.Parallel()
	var reply agentrpc.ChunkReply

	assert.Equal(t, model.TraceChunk{}, reply.GetChunk().Model(), "a reply without a chunk reads as the zero chunk")
}
