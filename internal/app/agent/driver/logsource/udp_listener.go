package logsource

import (
	"bytes"
	"context"
	"errors"
	"net"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/logs"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// maxDatagramBytes は UDP の 1 つのデータグラムの上限
const maxDatagramBytes = 64 << 10

// UDPListener はログルーターから、データグラムで送られてくるログを受ける。1 つのデータグラムに複数行あってもよい
type UDPListener struct {
	receiver *logs.Receiver
	conn     net.PacketConn
}

// NewUDPListener は待ち受けるアドレスが空なら、何も受けない
func NewUDPListener(cfg *config.Config, receiver *logs.Receiver) (*UDPListener, func(), error) {
	u := &UDPListener{receiver: receiver}
	if cfg.LogUDPAddr == "" {
		return u, func() {}, nil
	}
	conn, err := net.ListenPacket("udp", cfg.LogUDPAddr)
	if err != nil {
		return nil, nil, err
	}
	u.conn = conn
	return u, func() { _ = conn.Close() }, nil
}

func (u *UDPListener) Run(ctx context.Context) error {
	if u.conn == nil {
		<-ctx.Done()
		return nil
	}
	stop := context.AfterFunc(ctx, func() { _ = u.conn.Close() })
	defer stop()
	buf := make([]byte, maxDatagramBytes)
	for {
		n, _, err := u.conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		for line := range bytes.SplitSeq(buf[:n], []byte("\n")) {
			if line = trimEOL(line); len(line) > 0 {
				u.receiver.Receive(ctx, observation.LogSourceUDP, "", string(line))
			}
		}
	}
}
