package socket

import (
	"bufio"
	"context"
	"net"
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/protocol/agentsocket"
)

// MessageHandler は server から届いたメッセージと、接続が切れたことを受ける
type MessageHandler interface {
	HandleMessage(m agentsocket.Message)
	HandleDisconnect()
}

// SocketConn は server への 1 本の接続。書くのは複数の goroutine から、読むのは Run だけ
type SocketConn struct {
	conn      net.Conn
	writeMu   sync.Mutex
	finishMu  sync.Mutex
	finishing bool
	closed    chan struct{}
	closeOnce sync.Once
}

func NewSocketConn(addr string) (*SocketConn, func(), error) {
	conn, err := net.DialTimeout("unix", addr, time.Second)
	if err != nil {
		return nil, nil, err
	}
	c := &SocketConn{conn: conn, closed: make(chan struct{})}
	return c, c.Close, nil
}

func (c *SocketConn) Run(_ context.Context, h MessageHandler) error {
	r := bufio.NewReaderSize(c.conn, 64<<10)
	for {
		m, err := agentsocket.Read(r)
		if err != nil {
			select {
			case <-c.closed:
			default:
				h.HandleDisconnect()
			}
			c.Close()
			return nil
		}
		h.HandleMessage(m)
	}
}

func (c *SocketConn) Write(m agentsocket.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return agentsocket.Write(c.conn, m)
}

// BeginFinish は、終える手順をまだ誰も始めておらず、接続も切れていなければ true を返す
func (c *SocketConn) BeginFinish() bool {
	c.finishMu.Lock()
	defer c.finishMu.Unlock()
	select {
	case <-c.closed:
		return false
	default:
	}
	if c.finishing {
		return false
	}
	c.finishing = true
	return true
}

func (c *SocketConn) WaitFinished() {
	<-c.closed
}

func (c *SocketConn) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
	})
}
