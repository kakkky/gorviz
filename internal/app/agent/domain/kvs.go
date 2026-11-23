//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_kvs.go -package=mock
package domain

import (
	"context"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// TargetRegistry は観測するアプリの一覧。UpdateTargets は読んでから書くまでを、ほかの書き込みと重ねずに行う
type TargetRegistry interface {
	Targets(ctx context.Context) ([]TargetInstance, error)
	UpdateTargets(ctx context.Context, update func(current []TargetInstance) []TargetInstance) error
}

type TargetStateStore interface {
	TargetState(ctx context.Context, id TargetID) (TargetState, error)
	SetTargetState(ctx context.Context, id TargetID, state TargetState) error
	// RetainTargetStates は一覧から外れたアプリの状態を忘れる
	RetainTargetStates(ctx context.Context, ids []TargetID) error
}

type GoroutineMemoryStore interface {
	GoroutineMemory(ctx context.Context, in observation.Instance) (GoroutineMemory, error)
	SetGoroutineMemory(ctx context.Context, in observation.Instance, memory GoroutineMemory) error
}

type LogSequencer interface {
	NextLogSeq(ctx context.Context, in observation.Instance) (uint64, error)
}

type CursorStore interface {
	Cursor(ctx context.Context, in observation.Instance) (observation.Cursor, error)
	SetCursor(ctx context.Context, in observation.Instance, c observation.Cursor) error
}

// InstanceLifecycle は終わったインスタンス。送り切るまで覚え、送り切ったらそのインスタンスについて覚えていることをすべて忘れる
type InstanceLifecycle interface {
	MarkEnded(ctx context.Context, in observation.Instance) error
	EndedInstances(ctx context.Context) ([]observation.Instance, error)
	Forget(ctx context.Context, in observation.Instance) error
}
