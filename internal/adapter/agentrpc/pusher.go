package agentrpc

import (
	"context"
	"net/rpc"
	"time"

	"github.com/kakkky/gorviz/internal/domain"
)

const pushLogBatch = 5_000

// Pusher は agent の側で、軽いもの (ログ・goroutine dump) を interval ごとに server へ送る
type Pusher struct {
	interval time.Duration
	dumps    domain.DumpSource
	logs     domain.LogSource
}

func NewPusher(interval time.Duration, dumps domain.DumpSource, logs domain.LogSource) *Pusher {
	return &Pusher{interval: interval, dumps: dumps, logs: logs}
}

// pushUntilBroken は接続が切れるか ctx が終わるまで送り続ける。
// どこまで送ったかは接続ごとに持ち直し、つなぎ直したら手元にあるものを最初から送る (server が再起動していても取りこぼさない。重なったログは server が捨てる)
func (p *Pusher) pushUntilBroken(ctx context.Context, rc *rpc.Client) {
	var (
		lastLog    domain.LogLineID
		lastDumpAt time.Time
		ticker     = time.NewTicker(p.interval)
	)
	defer ticker.Stop()
	for {
		args := PushArgs{Logs: p.logs.LogsSince(lastLog, pushLogBatch)}
		if d, ok := p.dumps.LatestDump(); ok && d.At.After(lastDumpAt) {
			args.Dump = &d
		}
		if err := rc.Call(ingestServiceName+".Push", args, &Empty{}); err != nil {
			return
		}
		if n := len(args.Logs); n > 0 {
			lastLog = args.Logs[n-1].ID
		}
		if args.Dump != nil {
			lastDumpAt = args.Dump.At
		}
		if len(args.Logs) == pushLogBatch {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
