// Package push は agent の側で、push の接続の上で軽いもの (ログ・goroutine dump) を server へ送り続ける
package push

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/app/protocol/agentrpc"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

const pushLogBatch = 5_000

// Stream は interval ごとに、前に送ったものより新しいログと dump を送る
type Stream struct {
	interval             time.Duration
	getLatestDumpUsecase *usecase.GetLatestDumpUsecase
	listLogsSinceUsecase *usecase.ListLogsSinceUsecase
}

func NewStream(interval time.Duration, uc *usecase.UseCase) *Stream {
	return &Stream{interval: interval, getLatestDumpUsecase: uc.NewGetLatestDumpUsecase(), listLogsSinceUsecase: uc.NewListLogsSinceUsecase()}
}

// Run は接続が切れるか ctx が終わるまで送り続ける。
// どこまで送ったかは接続ごとに持ち直し、つなぎ直したら手元にあるものを最初から送る (server が再起動していても取りこぼさない。重なったログは server が捨てる)
func (p *Stream) Run(ctx context.Context, server agentrpc.IngestClient) {
	var (
		lastLog    model.LogLineID
		lastDumpAt time.Time
		ticker     = time.NewTicker(p.interval)
	)
	defer ticker.Stop()
	for {
		logs, err := p.listLogsSinceUsecase.Exec(ctx, usecase.ListLogsSinceUsecaseInput{After: lastLog, Limit: pushLogBatch})
		if err != nil {
			return
		}
		req := &agentrpc.PushRequest{Logs: agentrpc.NewLogLines(logs.Logs)}
		d, err := p.getLatestDumpUsecase.Exec(ctx, usecase.GetLatestDumpUsecaseInput{})
		newDump := err == nil && d.OK && d.Dump.At.After(lastDumpAt)
		if newDump {
			req.Dump = agentrpc.NewGoroutineDump(d.Dump)
		}
		if _, err := server.Push(ctx, req); err != nil {
			return
		}
		if n := len(logs.Logs); n > 0 {
			lastLog = logs.Logs[n-1].ID
		}
		if newDump {
			lastDumpAt = d.Dump.At
		}
		if len(logs.Logs) == pushLogBatch {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
