//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_runtime.go -package=mock
package domain

import (
	"context"
	"time"
)

// RawTrace は取ったままの execution trace。ReceivedAt は取り終えた時刻
type RawTrace struct {
	Data       []byte
	ReceivedAt time.Time
}

// RuntimeClient はアプリの runtime から取る。FetchGoroutineLeakProfile は、アプリの Go が goroutineleak プロファイルを持たなければ ErrNotFound
type RuntimeClient interface {
	FetchExecutionTrace(ctx context.Context, t TargetInstance, length time.Duration) (RawTrace, error)
	FetchThreadCreateProfile(ctx context.Context, t TargetInstance) ([]byte, error)
	FetchGoroutineLeakProfile(ctx context.Context, t TargetInstance) ([]byte, error)
}
