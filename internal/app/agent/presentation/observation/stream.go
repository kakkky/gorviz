// Package push は agent の側で、server への接続の上でログ・goroutine dump・読み込んだ trace の区切りを送り続ける
package push

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/app/protocol/agentrpc"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

const (
	pushLogBatch = 5_000
	// pushChunkBatch は 1 回で送る区切りの数。忙しい対象の区切りは 1 つで数 MB になるので、つなぎ直した直後に溜まった分をまとめて送らない
	pushChunkBatch = 4
)

// Stream は interval ごとに、前に送ったものより新しいログ・dump・区切りを送る
type Stream struct {
	interval                    time.Duration
	getLatestDumpUsecase        *usecase.GetLatestDumpUsecase
	listLogsSinceUsecase        *usecase.ListLogsSinceUsecase
	listTraceChunksSinceUsecase *usecase.ListTraceChunksSinceUsecase
}

func NewStream(interval time.Duration, uc *usecase.UseCase) *Stream {
	return &Stream{
		interval:                    interval,
		getLatestDumpUsecase:        uc.NewGetLatestDumpUsecase(),
		listLogsSinceUsecase:        uc.NewListLogsSinceUsecase(),
		listTraceChunksSinceUsecase: uc.NewListTraceChunksSinceUsecase(),
	}
}

// Run は接続が切れるか ctx が終わるまで送り続ける。
// どこまで送ったかは接続ごとに持ち直し、つなぎ直したら手元にあるものを最初から送る (server が再起動していても取りこぼさない。重なったログは server が捨てる)
func (p *Stream) Run(ctx context.Context, server agentrpc.IngestClient) {
	var (
		lastLog     model.LogLineID
		lastDumpAt  time.Time
		lastChunkAt time.Time
		ticker      = time.NewTicker(p.interval)
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
		chunks, err := p.listTraceChunksSinceUsecase.Exec(ctx, usecase.ListTraceChunksSinceUsecaseInput{After: lastChunkAt, Limit: pushChunkBatch})
		if err != nil {
			return
		}
		req.Chunks = agentrpc.NewTraceChunks(chunks.Chunks)
		if _, err := server.Push(ctx, req); err != nil {
			return
		}
		if n := len(logs.Logs); n > 0 {
			lastLog = logs.Logs[n-1].ID
		}
		if newDump {
			lastDumpAt = d.Dump.At
		}
		if n := len(chunks.Chunks); n > 0 {
			lastChunkAt = chunks.Chunks[n-1].StartedAt
		}
		if len(logs.Logs) == pushLogBatch || len(chunks.Chunks) == pushChunkBatch {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
