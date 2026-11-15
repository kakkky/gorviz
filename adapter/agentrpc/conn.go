package agentrpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/rpc"
	"time"

	"github.com/kakkky/gorviz/domain"
)

const redialInterval = 2 * time.Second

// Dial は agent の側で、server へ外向きにつなぎ、その接続の上で server からの呼び出しに答え続ける。
// 切れたらつなぎ直す。ctx が終わるまで返らない
func Dial(ctx context.Context, addr string, hello Hello, svc *Service) error {
	server := rpc.NewServer()
	if err := server.RegisterName(serviceName, svc); err != nil {
		return domain.ErrInternal.Wrap(err, "register the agent service")
	}
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
		server.ServeConn(conn)
		stop()
		slog.WarnContext(ctx, "disconnected from the gorviz server", slog.String("addr", addr))
		sleep(ctx, redialInterval)
	}
	return nil
}

// Accept は server の側で、agent からの接続を受けるたびに onAgent を呼ぶ。ctx が終わるまで返らない
func Accept(ctx context.Context, addr string, onAgent func(Hello, *rpc.Client)) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return domain.ErrUnavailable.Wrap(err, "listen for agents")
	}
	slog.InfoContext(ctx, "waiting for gorviz agents", slog.String("addr", addr))
	stop := context.AfterFunc(ctx, func() { ln.Close() })
	defer stop()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return domain.ErrUnavailable.Wrap(err, "accept an agent")
		}
		hello, err := readHello(conn)
		if err != nil {
			slog.WarnContext(ctx, "read the agent hello", slog.Any("err", err))
			conn.Close()
			continue
		}
		onAgent(hello, rpc.NewClient(conn))
	}
}

// readHello は 1 行だけ読む。後ろは RPC がそのまま使うので、余分に読み進めない
func readHello(conn net.Conn) (Hello, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		if _, err := conn.Read(buf); err != nil {
			return Hello{}, err
		}
		if buf[0] == '\n' {
			break
		}
		line = append(line, buf[0])
		if len(line) > 4096 {
			return Hello{}, domain.ErrInvalidArgument.With("the agent hello is too long")
		}
	}
	var h Hello
	if err := json.Unmarshal(line, &h); err != nil {
		return Hello{}, domain.ErrInvalidArgument.Wrap(err, "decode the agent hello")
	}
	return h, nil
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
