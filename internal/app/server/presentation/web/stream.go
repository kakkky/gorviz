package web

import (
	"context"
	"log/slog"
	"time"
)

// streamEvery は SSE の接続が切れるまで、every ごとに send を呼ぶ。描いた直後のページに送り直さないよう、最初は every 待ってから送る。
// send が失敗したら閉じる。ブラウザの EventSource は閉じられると繋ぎ直すので、一時的な失敗はそれで続きになる
func streamEvery(ctx context.Context, every time.Duration, send func(ctx context.Context) error) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := send(ctx); err != nil {
				if ctx.Err() == nil {
					slog.WarnContext(ctx, "stop a gui stream", slog.Any("err", err))
				}
				return
			}
		}
	}
}
