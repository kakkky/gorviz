package socket

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/kakkky/gorviz/internal/protocol/agent"
)

var ErrNotConnected = errors.New("the agent is not connected")

// SocketConn は agent との接続 1 本。セッションで観測するアプリは 1 つなので、つながる agent も 1 つ。
// 要求への応答は、要求を待っている側へ ID で返す
type SocketConn struct {
	mu           sync.Mutex
	conn         net.Conn
	connected    chan struct{}
	disconnected chan struct{}
	nextID       uint64
	waiting      map[uint64]chan agent.Response
}

func NewSocketConn() *SocketConn {
	return &SocketConn{connected: make(chan struct{}), disconnected: make(chan struct{}), waiting: map[uint64]chan agent.Response{}}
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

func (c *SocketConn) deliver(r agent.Response) {
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

// Send は要求を書く。応答を待たない要求 (finish) に使う
func (c *SocketConn) Send(in agent.Request) error {
	_, err := c.send(in, false)
	return err
}

func (c *SocketConn) send(in agent.Request, wait bool) (chan agent.Response, error) {
	if !c.IsConnected() {
		return nil, ErrNotConnected
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	in.ID = c.nextID
	var ch chan agent.Response
	if wait {
		ch = make(chan agent.Response, 1)
		c.waiting[in.ID] = ch
	}
	m, err := agent.Encode(agent.TypeRequest, in)
	if err != nil {
		return nil, err
	}
	if err := agent.Write(c.conn, m); err != nil {
		delete(c.waiting, in.ID)
		return nil, err
	}
	return ch, nil
}

// Request は要求を書いて応答を待つ
func (c *SocketConn) Request(ctx context.Context, in agent.Request) (agent.Response, error) {
	ch, err := c.send(in, true)
	if err != nil {
		return agent.Response{}, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return agent.Response{}, ErrNotConnected
		}
		return r, nil
	case <-ctx.Done():
		return agent.Response{}, ctx.Err()
	}
}
