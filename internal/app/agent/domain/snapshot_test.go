package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func TestBuildSnapshot(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Second)
	chanStack := []observation.Frame{
		{Function: "runtime.gopark"}, {Function: "runtime.chanrecv"}, {Function: "runtime.chanrecv1"},
		{Function: "main.worker", File: "/app/main.go", Line: 20},
	}
	creator := observation.Frame{Function: "main.main", File: "/app/main.go", Line: 10}
	chunk := observation.TraceChunk{StartedAt: start, Duration: 5 * time.Second, Entries: map[observation.GoroutineID]string{}}
	tests := []struct {
		name       string
		goroutines map[observation.GoroutineID]domain.GoroutineAtEnd
		memory     domain.GoroutineMemory
		want       observation.Goroutine
		wantMemory domain.RememberedGoroutine
	}{
		{
			name: "takes the wait reason, the wait start and the creator from the events in the chunk",
			goroutines: map[observation.GoroutineID]domain.GoroutineAtEnd{7: {
				Alive: true, State: observation.GoroutineWaiting, Determined: true, Since: start.Add(2 * time.Second),
				Reason: "chan receive", Stack: chanStack, CreatedBy: creator, HasCreatedBy: true,
			}},
			want: observation.Goroutine{
				ID: 7, State: observation.GoroutineWaiting, WaitReason: "chan receive", Waiting: 3 * time.Second,
				Entry: "main.worker", Current: chanStack[3], CreatedBy: creator, Stack: chanStack,
			},
			wantMemory: domain.RememberedGoroutine{State: observation.GoroutineWaiting, Since: start.Add(2 * time.Second), WaitReason: "chan receive", CreatedBy: creator, HasCreatedBy: true},
		},
		{
			name:       "carries over what it remembered from earlier chunks",
			goroutines: map[observation.GoroutineID]domain.GoroutineAtEnd{7: {Alive: true, State: observation.GoroutineWaiting, Stack: chanStack}},
			memory: domain.GoroutineMemory{7: {
				State: observation.GoroutineWaiting, Since: start.Add(-time.Minute), WaitReason: "chan receive", CreatedBy: creator, HasCreatedBy: true,
			}},
			want: observation.Goroutine{
				ID: 7, State: observation.GoroutineWaiting, WaitReason: "chan receive", Waiting: time.Minute + 5*time.Second,
				Entry: "main.worker", Current: chanStack[3], CreatedBy: creator, Stack: chanStack,
			},
			wantMemory: domain.RememberedGoroutine{State: observation.GoroutineWaiting, Since: start.Add(-time.Minute), WaitReason: "chan receive", CreatedBy: creator, HasCreatedBy: true},
		},
		{
			name:       "infers what it never saw from the stack and the entry",
			goroutines: map[observation.GoroutineID]domain.GoroutineAtEnd{7: {Alive: true, State: observation.GoroutineWaiting, Stack: chanStack}},
			want: observation.Goroutine{
				ID: 7, State: observation.GoroutineWaiting, WaitReason: "chan receive", WaitReasonInferred: true,
				Waiting: 5 * time.Second, WaitingLowerBound: true,
				Entry: "main.worker", Current: chanStack[3], CreatedBy: observation.Frame{Function: "main.worker"}, CreatedByInferred: true, Stack: chanStack,
			},
			wantMemory: domain.RememberedGoroutine{State: observation.GoroutineWaiting, Since: start, SinceLowerBound: true, WaitReason: "chan receive", WaitReasonInferred: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snap, memory := domain.BuildSnapshot(domain.ParsedTrace{Chunk: chunk, Goroutines: tt.goroutines}, tt.memory)
			assert.Equal(t, end, snap.At)
			assert.Equal(t, []observation.Goroutine{tt.want}, snap.Goroutines)
			assert.Equal(t, domain.GoroutineMemory{7: tt.wantMemory}, memory)
		})
	}
}

func TestBuildSnapshot_ForgetsEndedGoroutines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		goroutines map[observation.GoroutineID]domain.GoroutineAtEnd
		memory     domain.GoroutineMemory
	}{
		{
			name:       "drops a goroutine that ended in the chunk from the snapshot and the memory",
			goroutines: map[observation.GoroutineID]domain.GoroutineAtEnd{3: {Alive: false}},
			memory:     domain.GoroutineMemory{3: {State: observation.GoroutineWaiting}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snap, memory := domain.BuildSnapshot(domain.ParsedTrace{Goroutines: tt.goroutines}, tt.memory)
			assert.Empty(t, snap.Goroutines)
			assert.Empty(t, memory)
		})
	}
}

func TestInferWaitReason(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stack []string
		want  string
	}{
		{name: "reads a channel receive", stack: []string{"runtime.gopark", "runtime.chanrecv", "runtime.chanrecv1", "main.f"}, want: "chan receive"},
		{name: "reads a select", stack: []string{"runtime.gopark", "runtime.selectgo", "main.f"}, want: "select"},
		{name: "reads a mutex", stack: []string{"runtime.gopark", "runtime.goparkunlock", "runtime.semacquire1", "internal/sync.runtime_SemacquireMutex", "internal/sync.(*Mutex).lockSlow", "sync.(*Mutex).Lock", "main.f"}, want: "sync"},
		{name: "reads a sleep", stack: []string{"runtime.gopark", "time.Sleep", "main.f"}, want: "sleep"},
		{name: "reads a network wait", stack: []string{"runtime.gopark", "runtime.netpollblock", "internal/poll.runtime_pollWait", "net.(*conn).Read"}, want: "network"},
		{name: "gives up on an unknown wait", stack: []string{"runtime.gopark", "main.f"}, want: observation.UnknownWaitReason},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stack []observation.Frame
			for _, fn := range tt.stack {
				stack = append(stack, observation.Frame{Function: fn})
			}
			assert.Equal(t, tt.want, domain.InferWaitReason(stack))
		})
	}
}
