// Package agentlistener は server の側で、agent からの接続を待ち受ける。接続ごとに最初の 1 行の Hello を読み、向き (push・pull) で振り分ける
package agentlistener

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"

	"github.com/kakkky/gorviz/internal/app/api/agentrpc"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

// Endpoint は agent 1 つ分の受け口
type Endpoint struct {
	// Pull は pull の接続を受けたときに、その接続で agent を呼ぶ gRPC のクライアントを渡す
	Pull func(*grpc.ClientConn)
	// Ingest は push の接続の上で、agent からの呼び出しを受ける
	Ingest agentrpc.IngestServer
}

// Accept は agent からの接続を受ける。accept でその agent の受け口を引き、pull の接続なら Pull に渡し、
// push の接続なら Ingest に agent からの呼び出しを受けさせる。ctx が終わるまで返らない
func Accept(ctx context.Context, addr string, accept func(agentrpc.Hello) (Endpoint, bool)) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return model.ErrUnavailable.Wrap(err, "listen for agents")
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
			return model.ErrUnavailable.Wrap(err, "accept an agent")
		}
		hello, err := readHello(conn)
		if err == nil && hello.Kind != agentrpc.ConnPush && hello.Kind != agentrpc.ConnPull {
			err = model.ErrInvalidArgument.With(fmt.Sprintf("unknown connection kind %q", hello.Kind))
		}
		if err != nil {
			slog.WarnContext(ctx, "read the agent hello", slog.Any("err", err))
			conn.Close()
			continue
		}
		ep, ok := accept(hello)
		if !ok {
			conn.Close()
			continue
		}
		if hello.Kind == agentrpc.ConnPull {
			cc, err := agentrpc.NewClientConn(conn)
			if err != nil {
				slog.ErrorContext(ctx, "create a client for the agent", slog.String("agent", hello.Name), slog.Any("err", err))
				conn.Close()
				continue
			}
			ep.Pull(cc)
			continue
		}
		go servePush(ctx, conn, ep.Ingest)
	}
}

func servePush(ctx context.Context, conn net.Conn, ingest agentrpc.IngestServer) {
	defer conn.Close()
	agentrpc.Serve(ctx, conn, func(s grpc.ServiceRegistrar) { agentrpc.RegisterIngestServer(s, ingest) })
}

// readHello は 1 行だけ読む。後ろは gRPC がそのまま使うので、余分に読み進めない
func readHello(conn net.Conn) (agentrpc.Hello, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		if _, err := conn.Read(buf); err != nil {
			return agentrpc.Hello{}, err
		}
		if buf[0] == '\n' {
			break
		}
		line = append(line, buf[0])
		if len(line) > 4096 {
			return agentrpc.Hello{}, model.ErrInvalidArgument.With("the agent hello is too long")
		}
	}
	var h agentrpc.Hello
	if err := json.Unmarshal(line, &h); err != nil {
		return agentrpc.Hello{}, model.ErrInvalidArgument.Wrap(err, "decode the agent hello")
	}
	return h, nil
}
