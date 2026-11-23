//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_ring_buffer.go -package=mock
package domain

import (
	"context"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// ObservationBuffer は送るまでの観測データを、インスタンスごとに持つ。上限を超えたら古いものから捨てる
type ObservationBuffer interface {
	PushLogLine(ctx context.Context, in observation.Instance, l observation.LogLine) error
	PushTraceChunk(ctx context.Context, in observation.Instance, c observation.TraceChunk) error
	PushSnapshot(ctx context.Context, in observation.Instance, s observation.GoroutineSnapshot) error
	PushLeakCheck(ctx context.Context, in observation.Instance, l observation.GoroutineLeakCheck) error
	Instances(ctx context.Context) ([]observation.Instance, error)
	Buffered(ctx context.Context, in observation.Instance) (Buffered, error)
	// DropCovered は server が持っている分を捨て、残りを返す
	DropCovered(ctx context.Context, in observation.Instance, c observation.Cursor, agent observation.AgentID) (Buffered, error)
	Remove(ctx context.Context, in observation.Instance) error
}
