// Package serverconn は agent の側で、server へ外向きに接続を張る。切れたらつなぎ直す
package serverconn

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/agentrpc"
)

const redialInterval = 2 * time.Second

// Push は server への接続を張り、接続ごとに stream を回す。stream は接続が切れたら返す。ctx が終わるまで返らない
func Push(ctx context.Context, addr string, hello agentrpc.Hello, stream func(context.Context, agentrpc.IngestClient)) error {
	dialLoop(ctx, addr, hello, func(conn net.Conn) {
		cc, err := agentrpc.NewClientConn(conn)
		if err != nil {
			slog.ErrorContext(ctx, "create a client for the gorviz server", slog.Any("err", err))
			return
		}
		stream(ctx, agentrpc.NewIngestClient(cc))
		cc.Close()
	})
	return nil
}

func dialLoop(ctx context.Context, addr string, hello agentrpc.Hello, use func(net.Conn)) {
	for ctx.Err() == nil {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			slog.WarnContext(ctx, "connect to the gorviz server", slog.String("addr", addr), slog.Any("err", err))
			sleep(ctx, redialInterval)
			continue
		}
		slog.InfoContext(ctx, "connected to the gorviz server", slog.String("addr", addr), slog.String("name", hello.Name))
		b, _ := json.Marshal(hello)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			conn.Close()
			continue
		}
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		use(conn)
		stop()
		conn.Close()
		slog.WarnContext(ctx, "disconnected from the gorviz server", slog.String("addr", addr))
		sleep(ctx, redialInterval)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
