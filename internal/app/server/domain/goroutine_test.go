package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestCauseOfPlace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		place    observation.Place
		function string
		want     domain.MainCause
	}{
		{name: "running is RUNNING", place: observation.Place{Kind: observation.PlaceRunning}, want: domain.CauseRunning},
		{name: "a run queue is RUNNABLE", place: observation.Place{Kind: observation.PlaceGlobalQueue}, want: domain.CauseRunnable},
		{name: "a sync wait in Mutex.Lock is LOCK_WAIT", place: observation.Place{Kind: observation.PlaceWaiting, Reason: "sync"}, function: "sync.(*Mutex).Lock", want: domain.CauseLockWait},
		{name: "a sync wait in RWMutex.RLock is LOCK_WAIT", place: observation.Place{Kind: observation.PlaceWaiting, Reason: "sync"}, function: "sync.(*RWMutex).RLock", want: domain.CauseLockWait},
		{name: "a sync wait in WaitGroup.Wait is SYNC_WAIT", place: observation.Place{Kind: observation.PlaceWaiting, Reason: "sync"}, function: "sync.(*WaitGroup).Wait", want: domain.CauseSyncWait},
		{name: "a channel receive is SYNC_WAIT", place: observation.Place{Kind: observation.PlaceWaiting, Reason: "chan receive"}, want: domain.CauseSyncWait},
		{name: "network is IO_WAIT", place: observation.Place{Kind: observation.PlaceWaiting, Reason: "network"}, want: domain.CauseIOWait},
		{name: "a syscall is IO_WAIT", place: observation.Place{Kind: observation.PlaceSyscall}, want: domain.CauseIOWait},
		{name: "a syscall in cgo is CGO", place: observation.Place{Kind: observation.PlaceSyscall, Cgo: true}, want: domain.CauseCgo},
		{name: "sleep is SLEEP", place: observation.Place{Kind: observation.PlaceWaiting, Reason: "sleep"}, want: domain.CauseSleep},
		{name: "a gc wait is GC", place: observation.Place{Kind: observation.PlaceWaiting, Reason: "GC mark assist wait"}, want: domain.CauseGC},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := domain.CauseOfPlace(tt.place, tt.function)
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCauseOfSnapshot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		g    observation.Goroutine
		want domain.MainCause
	}{
		{name: "a semacquire under Mutex.Lock is LOCK_WAIT", g: observation.Goroutine{State: observation.GoroutineWaiting, WaitReason: "sync.Mutex.Lock", Stack: []observation.Frame{{Function: "sync.(*Mutex).Lock"}}}, want: domain.CauseLockWait},
		{name: "a semacquire under WaitGroup.Wait is SYNC_WAIT", g: observation.Goroutine{State: observation.GoroutineWaiting, WaitReason: "sync.WaitGroup.Wait", Stack: []observation.Frame{{Function: "sync.(*WaitGroup).Wait"}}}, want: domain.CauseSyncWait},
		{name: "a syscall under cgocall is CGO", g: observation.Goroutine{State: observation.GoroutineSyscall, Stack: []observation.Frame{{Function: "runtime.cgocall"}}}, want: domain.CauseCgo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.CauseOfSnapshot(tt.g))
		})
	}
}

func TestBreakdown(t *testing.T) {
	t.Parallel()
	c := observation.TraceChunk{Duration: 10 * time.Second, Events: []observation.StateChange{
		{At: 0, Kind: observation.StateChangeGoroutine, Goroutine: 1, Place: observation.Place{Kind: observation.PlaceRunning}},
		{At: 2 * time.Second, Kind: observation.StateChangeGoroutine, Goroutine: 1, Place: observation.Place{Kind: observation.PlaceWaiting, Reason: "sync"}, Function: "sync.(*Mutex).Lock", Location: "a.go:3"},
		{At: 8 * time.Second, Kind: observation.StateChangeGoroutine, Goroutine: 1, Place: observation.Place{Kind: observation.PlaceRunning}, WokenBy: 2},
	}, GC: []observation.GCRange{{Kind: observation.GCMarkAssist, Goroutine: 1, Start: 9 * time.Second, End: 10 * time.Second}}}
	tests := []struct {
		name     string
		from, to time.Duration
		want     map[domain.MainCause]time.Duration
		wantMain domain.MainCause
	}{
		{name: "splits the whole chunk with mark assist counted as GC", from: 0, to: 10 * time.Second,
			want: map[domain.MainCause]time.Duration{domain.CauseRunning: 3 * time.Second, domain.CauseLockWait: 6 * time.Second, domain.CauseGC: time.Second}, wantMain: domain.CauseLockWait},
		{name: "cuts out the period", from: time.Second, to: 3 * time.Second,
			want: map[domain.MainCause]time.Duration{domain.CauseRunning: time.Second, domain.CauseLockWait: time.Second}, wantMain: domain.CauseRunning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := domain.Breakdown(c, 1, tt.from, tt.to)
			assert.Equal(t, tt.want, b.Durations)
			main, ok := b.Main()
			require.True(t, ok)
			assert.Equal(t, tt.wantMain, main)
		})
	}
}

func TestMatchLeaks(t *testing.T) {
	t.Parallel()
	stack := []observation.Frame{{Function: "runtime.gopark"}, {Function: "main.send", File: "main.go", Line: 9}}
	other := []observation.Frame{{Function: "main.other", File: "main.go", Line: 20}}
	g := func(id observation.GoroutineID, waiting time.Duration, s []observation.Frame) observation.Goroutine {
		return observation.Goroutine{ID: id, State: observation.GoroutineWaiting, Waiting: waiting, Stack: s}
	}
	tests := []struct {
		name     string
		count    int
		snapshot []observation.Goroutine
		want     []observation.GoroutineID
		wantOf   int
		wantCert domain.Certainty
	}{
		{name: "marks all as leaked when exactly the count share the stack", count: 2, snapshot: []observation.Goroutine{g(1, time.Second, stack), g(2, time.Minute, stack), g(3, time.Hour, other)},
			want: []observation.GoroutineID{2, 1}, wantOf: 2, wantCert: domain.CertaintyFact},
		{name: "picks the longest waiting when more share the stack", count: 1, snapshot: []observation.Goroutine{g(1, time.Second, stack), g(2, time.Minute, stack)},
			want: []observation.GoroutineID{2}, wantOf: 2, wantCert: domain.CertaintyInference},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lc := observation.GoroutineLeakCheck{Status: observation.LeakCheckDone, Leaks: []observation.LeakedStack{{Count: tt.count, Stack: stack}}}
			ms := domain.MatchLeaks(lc, observation.GoroutineSnapshot{Goroutines: tt.snapshot})
			require.Len(t, ms, 1)
			assert.Equal(t, tt.want, ms[0].Goroutines)
			assert.Equal(t, tt.wantOf, ms[0].Of)
			assert.Equal(t, tt.count, ms[0].Count)
			assert.Equal(t, tt.wantCert, ms[0].Certainty)
		})
	}
}

func TestMatchLogWriter(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	chunk := observation.TraceChunk{StartedAt: start, Duration: 5 * time.Second,
		LogMarks: []observation.LogMark{{Seq: 7, Goroutine: 42, At: time.Second}},
		Procs: []observation.ProcLane{
			{Proc: 0, Spans: []observation.ProcSpan{{Goroutine: 5, Start: 0, End: 2 * time.Second}}},
			{Proc: 1, Spans: []observation.ProcSpan{{Goroutine: 6, Start: time.Second, End: 4 * time.Second}}},
		}}
	tests := []struct {
		name      string
		log       observation.LogLine
		wantG     observation.GoroutineID
		wantCert  domain.Certainty
		wantFound bool
	}{
		{name: "binds by the logger adapter number as a fact", log: observation.LogLine{At: start.Add(time.Second), TraceSeq: 7, HasTraceSeq: true}, wantG: 42, wantCert: domain.CertaintyFact, wantFound: true},
		{name: "infers the only running goroutine without a number", log: observation.LogLine{At: start.Add(500 * time.Millisecond)}, wantG: 5, wantCert: domain.CertaintyInference, wantFound: true},
		{name: "does not bind while several goroutines run", log: observation.LogLine{At: start.Add(1500 * time.Millisecond)}},
		{name: "does not bind outside the chunk", log: observation.LogLine{At: start.Add(time.Minute)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := domain.MatchLogWriter("a", tt.log, []observation.TraceChunk{chunk})
			assert.Equal(t, tt.wantFound, w.Found)
			assert.Equal(t, tt.wantG, w.Goroutine)
			assert.Equal(t, tt.wantCert, w.Certainty)
		})
	}
}
