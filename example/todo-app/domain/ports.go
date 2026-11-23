//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_ports.go -package=mock
package domain

import (
	"context"
	"time"
)

type TodoAccessor interface {
	List(ctx context.Context) ([]Todo, error)
	Add(ctx context.Context, title Title, reminder bool, now time.Time) (Todo, error)
	Complete(ctx context.Context, id TodoID, now time.Time) (Todo, error)
	Delete(ctx context.Context, id TodoID) error
	Count(ctx context.Context) (int, error)
	// WithAllLocked は fn が返るまで、ほかの読み書きを待たせる
	WithAllLocked(ctx context.Context, fn func([]Todo) error) error
}

type BackupFileAccessor interface {
	Write(ctx context.Context, data []byte) (string, error)
	LastWrittenAt(ctx context.Context) (time.Time, bool)
}

type NotifierClient interface {
	RegisterReminder(ctx context.Context, t Todo) error
}

type MailClient interface {
	SendCompleted(ctx context.Context, t Todo) error
}
