package web

import (
	"context"
	"time"
)

// streamEvery は SSE の接続が切れるまで、every ごとに send を呼ぶ。send が失敗したら閉じる
func streamEvery(ctx context.Context, every time.Duration, send func(ctx context.Context) error) {
	t := time.NewTimer(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := send(ctx); err != nil {
				return
			}
			t.Reset(every)
		}
	}
}
