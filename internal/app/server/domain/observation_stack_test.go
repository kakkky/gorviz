package domain_test

import (
	"testing"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/stretchr/testify/assert"
)

func TestIssueFunction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, fn, want string
	}{
		{name: "keeps a named function", fn: "main.(*Server).handle", want: "main.(*Server).handle"},
		{name: "folds a closure into the outer function", fn: "main.(*Server).handle.func1.2", want: "main.(*Server).handle"},
		{name: "folds a go statement wrapper", fn: "main.serve.gowrap1", want: "main.serve"},
		{name: "folds type parameters", fn: "main.Map[go.shape.int,go.shape.string]", want: "main.Map[...]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.IssueFunction(tt.fn))
		})
	}
}

func TestCauseOfWait(t *testing.T) {
	t.Parallel()
	lock := []domain.Frame{{Function: "sync.(*Mutex).Lock"}, {Function: "main.handle"}}
	wg := []domain.Frame{{Function: "sync.(*WaitGroup).Wait"}, {Function: "main.handle"}}
	tests := []struct {
		name   string
		reason string
		stack  []domain.Frame
		want   domain.MainCause
	}{
		{name: "tells a Lock wait from other sync waits by the stack", reason: "sync", stack: lock, want: domain.CauseLockWait},
		{name: "treats a WaitGroup wait as waiting for other goroutines", reason: "sync", stack: wg, want: domain.CauseSyncWait},
		{name: "treats the network as IO", reason: "network", want: domain.CauseIOWait},
		{name: "treats a sleep as SLEEP", reason: "sleep", want: domain.CauseSleep},
		{name: "treats a channel as waiting for other goroutines", reason: "chan receive", want: domain.CauseSyncWait},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.CauseOfWait(tt.reason, tt.stack))
		})
	}
}
