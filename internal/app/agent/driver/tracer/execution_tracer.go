package tracer

import (
	"context"
	"io"
	"runtime/trace"
	"sync"
	"sync/atomic"

	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// ExecutionTracer は execution trace を io.Pipe に書かせ、読む側に流す。trace.Stop は書き出しが読み切られるまで戻らないので、止める間も読み続ける
type ExecutionTracer struct {
	r         *io.PipeReader
	w         *io.PipeWriter
	stopOnce  sync.Once
	drained   chan struct{}
	drainOnce sync.Once
	leakSeq   atomic.Uint64
}

func NewExecutionTracer() (*ExecutionTracer, func()) {
	r, w := io.Pipe()
	t := &ExecutionTracer{r: r, w: w, drained: make(chan struct{})}
	return t, t.Stop
}

func (t *ExecutionTracer) Start() error {
	return trace.Start(t.w)
}

func (t *ExecutionTracer) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil {
		t.drainOnce.Do(func() { close(t.drained) })
	}
	return n, err
}

func (t *ExecutionTracer) Stop() {
	t.stopOnce.Do(func() {
		if trace.IsEnabled() {
			trace.Stop()
		}
		_ = t.w.Close()
	})
}

func (t *ExecutionTracer) Drained() <-chan struct{} {
	return t.drained
}

func (t *ExecutionTracer) Log(event string) {
	trace.Log(context.Background(), agent.TraceCategory, event)
}

func (t *ExecutionTracer) NextLeakCheckSeq() uint64 {
	return t.leakSeq.Add(1)
}
