// Package worker は観測データから作るものを、usecase ごとに間隔を空けて作り続ける
package worker

import (
	"context"
	"log/slog"
	"time"
)

// loop は exec が終わるたびに interval だけ待ち直して次を呼ぶ。exec のエラーではログに出して続ける
func loop(ctx context.Context, name string, interval time.Duration, exec func(ctx context.Context, now time.Time) error) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if err := exec(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "worker exec", slog.String("worker", name), slog.Any("err", err))
		}
		timer.Reset(interval)
	}
}
