package worker

import (
	"context"
	"time"
)

// wait は前の Exec が終わってから間隔を待つ。ctx が終わったら false
func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
