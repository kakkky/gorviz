package socket

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/protocol/agentsocket"
)

// MessageHandler は agent から届いた観測データと、接続の終わりを受ける。clean は終わりの合図を受けてから切れたこと
type MessageHandler interface {
	HandleMessage(ctx context.Context, m agentsocket.Message)
	HandleDisconnect(ctx context.Context, clean bool)
}

// SocketListener は agent の接続を unix socket で受ける。手元のマシンの中からしかつなげない
type SocketListener struct {
	path     string
	listener net.Listener
	conn     *SocketConn
	handler  MessageHandler
}

func NewSocketListener(path string, conn *SocketConn, handler MessageHandler) (*SocketListener, func(), error) {
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = l.Close()
		_ = os.Remove(path)
	}
	return &SocketListener{path: path, listener: l, conn: conn, handler: handler}, cleanup, nil
}

// Run は ctx が終わったら待ち受けを閉じる。つながっている agent からは、アプリの shutdown の分まで読み切る
func (s *SocketListener) Run(ctx context.Context) error {
	return scope.Run(context.WithoutCancel(ctx), func(sc *scope.Scope) error {
		accepted := make(chan struct{})
		sc.Go(func(context.Context) error {
			select {
			case <-ctx.Done():
			case <-accepted:
			}
			_ = s.listener.Close()
			return nil
		})
		sc.Go(func(context.Context) error {
			defer close(accepted)
			return s.accept(ctx)
		})
		return nil
	})
}

func (s *SocketListener) accept(ctx context.Context) error {
	for {
		c, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if !s.conn.attach(c) {
			_ = c.Close()
			continue
		}
		s.serve(ctx, c)
	}
}

func (s *SocketListener) serve(ctx context.Context, c net.Conn) {
	defer c.Close()
	r := bufio.NewReaderSize(c, 256<<10)
	clean := false
	for {
		m, err := agentsocket.Read(r)
		if err != nil {
			break
		}
		switch m.Kind {
		case agentsocket.KindReply:
			if reply, err := agentsocket.Decode[agentsocket.Reply](m); err == nil {
				s.conn.deliver(reply)
			}
			continue
		case agentsocket.KindBye:
			clean = true
		}
		s.handler.HandleMessage(ctx, m)
	}
	s.conn.detach()
	s.handler.HandleDisconnect(ctx, clean)
}
