package logsource

import (
	"bufio"
	"context"
	"errors"
	"net"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/logs"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// TCPListener はログルーター (Fluent Bit など) から、1 行ずつ送られてくるログを受ける
type TCPListener struct {
	receiver *logs.Receiver
	ln       net.Listener
	maxBytes int
}

// NewTCPListener は待ち受けるアドレスが空なら、何も受けない
func NewTCPListener(cfg *config.Config, receiver *logs.Receiver) (*TCPListener, func(), error) {
	t := &TCPListener{receiver: receiver, maxBytes: cfg.LogMaxLineBytes}
	if cfg.LogTCPAddr == "" {
		return t, func() {}, nil
	}
	ln, err := net.Listen("tcp", cfg.LogTCPAddr)
	if err != nil {
		return nil, nil, err
	}
	t.ln = ln
	return t, func() { _ = ln.Close() }, nil
}

func (t *TCPListener) Run(ctx context.Context) error {
	if t.ln == nil {
		<-ctx.Done()
		return nil
	}
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(ctx context.Context) error {
			<-ctx.Done()
			_ = t.ln.Close()
			return nil
		})
		for {
			conn, err := t.ln.Accept()
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return nil
				}
				continue
			}
			sc.Go(func(ctx context.Context) error {
				t.serve(ctx, conn)
				return nil
			})
		}
	})
}

func (t *TCPListener) serve(ctx context.Context, conn net.Conn) {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64<<10), t.maxBytes)
	for sc.Scan() {
		t.receiver.Receive(ctx, observation.LogSourceTCP, "", string(trimEOL(sc.Bytes())))
	}
}
