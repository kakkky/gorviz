package accessor

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/ringbuffer"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// RingBufferAccessor は送るまでの観測データを、インスタンスをキーにリングバッファに積む
type RingBufferAccessor struct {
	buf *ringbuffer.RingBuffer[observation.Instance]
}

func NewRingBufferAccessor(buf *ringbuffer.RingBuffer[observation.Instance]) *RingBufferAccessor {
	return &RingBufferAccessor{buf: buf}
}

func (a *RingBufferAccessor) PushLogLine(_ context.Context, in observation.Instance, l observation.LogLine) error {
	a.buf.Append(in, l, domain.LogLineBytes(l))
	return nil
}

func (a *RingBufferAccessor) PushTraceChunk(_ context.Context, in observation.Instance, c observation.TraceChunk) error {
	a.buf.Append(in, c, domain.TraceChunkBytes(c))
	return nil
}

func (a *RingBufferAccessor) PushSnapshot(_ context.Context, in observation.Instance, s observation.GoroutineSnapshot) error {
	a.buf.Append(in, s, domain.SnapshotBytes(s))
	return nil
}

func (a *RingBufferAccessor) PushLeakCheck(_ context.Context, in observation.Instance, l observation.GoroutineLeakCheck) error {
	a.buf.Append(in, l, domain.LeakCheckBytes(l))
	return nil
}

func (a *RingBufferAccessor) Instances(context.Context) ([]observation.Instance, error) {
	return a.buf.Keys(), nil
}

func (a *RingBufferAccessor) Buffered(_ context.Context, in observation.Instance) (domain.Buffered, error) {
	return toBuffered(a.buf.Values(in)), nil
}

func (a *RingBufferAccessor) DropCovered(_ context.Context, in observation.Instance, c observation.Cursor, agent observation.AgentID) (domain.Buffered, error) {
	rest := a.buf.RemoveIf(in, func(v any) bool {
		switch v := v.(type) {
		case observation.LogLine:
			return domain.LogCovered(c, agent, v)
		case observation.TraceChunk:
			return domain.TraceChunkCovered(c, v)
		case observation.GoroutineSnapshot:
			return domain.SnapshotCovered(c, v)
		case observation.GoroutineLeakCheck:
			return domain.LeakCheckCovered(c, v)
		}
		return true
	})
	return toBuffered(rest), nil
}

func (a *RingBufferAccessor) Remove(_ context.Context, in observation.Instance) error {
	a.buf.Delete(in)
	return nil
}

func toBuffered(values []any) domain.Buffered {
	var b domain.Buffered
	for _, v := range values {
		switch v := v.(type) {
		case observation.LogLine:
			b.Logs = append(b.Logs, v)
		case observation.TraceChunk:
			b.TraceChunks = append(b.TraceChunks, v)
		case observation.GoroutineSnapshot:
			b.Snapshots = append(b.Snapshots, v)
		case observation.GoroutineLeakCheck:
			b.LeakChecks = append(b.LeakChecks, v)
		}
	}
	return b
}
