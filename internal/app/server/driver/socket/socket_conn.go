package socket

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/kakkky/gorviz/internal/protocol/agentsocket"
)

var ErrNotConnected = errors.New("the agent is not connected")

// SocketConn は agent との接続 1 本。セッションで観測するアプリは 1 つなので、つながる agent も 1 つ。
// 指示への返事は、指示を待っている側へ ID で返す
type SocketConn struct {
	mu           sync.Mutex
	conn         net.Conn
	connected    chan struct{}
	disconnected chan struct{}
	nextID       uint64
	waiting      map[uint64]chan agentsocket.Reply
}

func NewSocketConn() *SocketConn {
	return &SocketConn{connected: make(chan struct{}), disconnected: make(chan struct{}), waiting: map[uint64]chan agentsocket.Reply{}}
}

func (c *SocketConn) attach(conn net.Conn) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return false
	}
	c.conn = conn
	close(c.connected)
	return true
}

func (c *SocketConn) detach() {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.disconnected:
	default:
		close(c.disconnected)
	}
	for id, ch := range c.waiting {
		close(ch)
		delete(c.waiting, id)
	}
}

func (c *SocketConn) deliver(r agentsocket.Reply) {
	c.mu.Lock()
	ch, ok := c.waiting[r.ID]
	delete(c.waiting, r.ID)
	c.mu.Unlock()
	if ok {
		ch <- r
	}
}

func (c *SocketConn) Connected() <-chan struct{} { return c.connected }

func (c *SocketConn) Disconnected() <-chan struct{} { return c.disconnected }

func (c *SocketConn) IsConnected() bool {
	select {
	case <-c.connected:
	default:
		return false
	}
	select {
	case <-c.disconnected:
		return false
	default:
		return true
	}
}

// Send は指示を書く。返事を待たない指示 (finish) に使う
func (c *SocketConn) Send(in agentsocket.Instruction) error {
	_, err := c.send(in, false)
	return err
}

func (c *SocketConn) send(in agentsocket.Instruction, wait bool) (chan agentsocket.Reply, error) {
	if !c.IsConnected() {
		return nil, ErrNotConnected
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	in.ID = c.nextID
	var ch chan agentsocket.Reply
	if wait {
		ch = make(chan agentsocket.Reply, 1)
		c.waiting[in.ID] = ch
	}
	m, err := agentsocket.Encode(agentsocket.KindInstruction, in)
	if err != nil {
		return nil, err
	}
	if err := agentsocket.Write(c.conn, m); err != nil {
		delete(c.waiting, in.ID)
		return nil, err
	}
	return ch, nil
}

// Request は指示を書いて返事を待つ
func (c *SocketConn) Request(ctx context.Context, in agentsocket.Instruction) (agentsocket.Reply, error) {
	ch, err := c.send(in, true)
	if err != nil {
		return agentsocket.Reply{}, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return agentsocket.Reply{}, ErrNotConnected
		}
		return r, nil
	case <-ctx.Done():
		return agentsocket.Reply{}, ctx.Err()
	}
}
