package agentrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/rpc"
	"time"

	"github.com/kakkky/gorviz/internal/domain"
)

const redialInterval = 2 * time.Second

// ServePull は agent の側で、pull の接続を張り、その上で server からの trace の要求に答え続ける。
// 切れたらつなぎ直す。ctx が終わるまで返らない
func ServePull(ctx context.Context, addr string, hello Hello, svc *Service) error {
	server := rpc.NewServer()
	if err := server.RegisterName(agentServiceName, svc); err != nil {
		return domain.ErrInternal.Wrap(err, "register the agent service")
	}
	hello.Kind = ConnPull
	dialLoop(ctx, addr, hello, func(conn net.Conn) { server.ServeConn(conn) })
	return nil
}

// Push は agent の側で、push の接続を張り、軽いものを送り続ける。切れたらつなぎ直す。ctx が終わるまで返らない
func Push(ctx context.Context, addr string, hello Hello, p *Pusher) error {
	hello.Kind = ConnPush
	dialLoop(ctx, addr, hello, func(conn net.Conn) {
		rc := rpc.NewClient(conn)
		p.pushUntilBroken(ctx, rc)
		rc.Close()
	})
	return nil
}

func dialLoop(ctx context.Context, addr string, hello Hello, use func(net.Conn)) {
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

// Accept は server の側で、agent からの接続を受ける。clientFor でその agent の Client を引き、
// pull の接続なら trace を求める口として Client に渡し、push の接続なら送られてくるものを Client に受け取らせる。ctx が終わるまで返らない
func Accept(ctx context.Context, addr string, clientFor func(Hello) (*Client, bool)) error {
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
		if err == nil && hello.Kind != ConnPush && hello.Kind != ConnPull {
			err = domain.ErrInvalidArgument.With(fmt.Sprintf("unknown connection kind %q", hello.Kind))
		}
		if err != nil {
			slog.WarnContext(ctx, "read the agent hello", slog.Any("err", err))
			conn.Close()
			continue
		}
		c, ok := clientFor(hello)
		if !ok {
			conn.Close()
			continue
		}
		if hello.Kind == ConnPull {
			c.SetConn(rpc.NewClient(conn))
			continue
		}
		go servePush(ctx, conn, c)
	}
}

func servePush(ctx context.Context, conn net.Conn, c *Client) {
	server := rpc.NewServer()
	if err := server.RegisterName(ingestServiceName, &ingest{c: c}); err != nil {
		slog.ErrorContext(ctx, "register the ingest service", slog.Any("err", err))
		conn.Close()
		return
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	server.ServeConn(conn)
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
