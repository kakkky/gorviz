package client

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

type ClockClient struct{}

func NewClockClient() (*ClockClient, error) { return &ClockClient{}, nil }

func (ClockClient) Now() time.Time { return time.Now() }

func (ClockClient) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return domain.ErrInternal.Wrap(ctx.Err(), "sleep")
	}
}
