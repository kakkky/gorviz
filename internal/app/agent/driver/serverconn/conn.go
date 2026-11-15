// Package serverconn は agent の側で、server へ外向きに接続を張る。切れたらつなぎ直す。
// push の接続では stream に RPC のクライアントを渡し、pull の接続では server からの呼び出しを rcvr に受けさせる
package serverconn

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/rpc"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/agentrpc"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

const redialInterval = 2 * time.Second

// ServePull は pull の接続を張り、その上で server からの呼び出しを rcvr に受けさせ続ける。ctx が終わるまで返らない
func ServePull(ctx context.Context, addr string, hello agentrpc.Hello, rcvr any) error {
	server := rpc.NewServer()
	if err := server.RegisterName(agentrpc.AgentServiceName, rcvr); err != nil {
		return model.ErrInternal.Wrap(err, "register the agent service")
	}
	hello.Kind = agentrpc.ConnPull
	dialLoop(ctx, addr, hello, func(conn net.Conn) { server.ServeConn(conn) })
	return nil
}

// Push は push の接続を張り、接続ごとに stream を回す。stream は接続が切れたら返す。ctx が終わるまで返らない
func Push(ctx context.Context, addr string, hello agentrpc.Hello, stream func(context.Context, *rpc.Client)) error {
	hello.Kind = agentrpc.ConnPush
	dialLoop(ctx, addr, hello, func(conn net.Conn) {
		rc := rpc.NewClient(conn)
		stream(ctx, rc)
		rc.Close()
	})
	return nil
}

func dialLoop(ctx context.Context, addr string, hello agentrpc.Hello, use func(net.Conn)) {
	for ctx.Err() == nil {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			slog.WarnContext(ctx, "connect to the gorviz server", slog.String("addr", addr), slog.String("kind", string(hello.Kind)), slog.Any("err", err))
			sleep(ctx, redialInterval)
			continue
		}
		slog.InfoContext(ctx, "connected to the gorviz server", slog.String("addr", addr), slog.String("name", hello.Name), slog.String("kind", string(hello.Kind)))
		b, _ := json.Marshal(hello)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			conn.Close()
			continue
		}
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		use(conn)
		stop()
		conn.Close()
		slog.WarnContext(ctx, "disconnected from the gorviz server", slog.String("addr", addr), slog.String("kind", string(hello.Kind)))
		sleep(ctx, redialInterval)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
