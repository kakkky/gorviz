package client

import (
	"context"
	"errors"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/socket"
	"github.com/kakkky/gorviz/internal/protocol/agentsocket"
)

// AgentClient は agent に指示を送り、返事を受け取る
type AgentClient struct {
	conn *socket.SocketConn
}

func NewAgentClient(conn *socket.SocketConn) (*AgentClient, error) {
	return &AgentClient{conn: conn}, nil
}

func (c *AgentClient) WaitConnected(ctx context.Context) error {
	select {
	case <-c.conn.Connected():
		return nil
	case <-ctx.Done():
		return domain.ErrInternal.Wrap(ctx.Err(), "wait for the agent")
	}
}

func (c *AgentClient) Connected() bool { return c.conn.IsConnected() }

func (c *AgentClient) WaitDisconnected(ctx context.Context) error {
	select {
	case <-c.conn.Disconnected():
		return nil
	case <-ctx.Done():
		return domain.ErrInternal.Wrap(ctx.Err(), "wait for the agent to disconnect")
	}
}

func (c *AgentClient) request(ctx context.Context, in agentsocket.Instruction) (agentsocket.Reply, error) {
	r, err := c.conn.Request(ctx, in)
	if errors.Is(err, socket.ErrNotConnected) {
		return agentsocket.Reply{}, domain.ErrInvalidArgument.Wrap(err, "the app is not running with the agent")
	}
	if err != nil {
		return agentsocket.Reply{}, domain.ErrInternal.Wrap(err, "instruct the agent "+string(in.Op))
	}
	if r.Error != "" {
		return agentsocket.Reply{}, domain.ErrInternal.With("the agent failed to " + string(in.Op) + ": " + r.Error)
	}
	return r, nil
}

func (c *AgentClient) WriteTraceEvent(ctx context.Context, event string) error {
	_, err := c.request(ctx, agentsocket.Instruction{Op: agentsocket.OpWriteTraceEvent, Event: event})
	return err
}

func (c *AgentClient) ResetCoverage(ctx context.Context) error {
	_, err := c.request(ctx, agentsocket.Instruction{Op: agentsocket.OpResetCoverage})
	return err
}

func (c *AgentClient) ReadCoverage(ctx context.Context) ([]domain.CoverageFile, error) {
	r, err := c.request(ctx, agentsocket.Instruction{Op: agentsocket.OpWriteCoverage})
	if err != nil {
		return nil, err
	}
	out := make([]domain.CoverageFile, 0, len(r.Coverage))
	for _, f := range r.Coverage {
		out = append(out, domain.CoverageFile{Name: f.Name, Data: f.Data})
	}
	return out, nil
}

func (c *AgentClient) RequestGoroutineLeaks(ctx context.Context) error {
	_, err := c.request(ctx, agentsocket.Instruction{Op: agentsocket.OpWriteGoroutineLeak})
	return err
}

func (c *AgentClient) RequestThreadCount(ctx context.Context) error {
	_, err := c.request(ctx, agentsocket.Instruction{Op: agentsocket.OpWriteThreadCount})
	return err
}

// Finish は返事を待たない。agent は最後の観測データと終わりの合図を送ってから接続を閉じる
func (c *AgentClient) Finish(_ context.Context) error {
	if err := c.conn.Send(agentsocket.Instruction{Op: agentsocket.OpFinish}); err != nil && !errors.Is(err, socket.ErrNotConnected) {
		return domain.ErrInternal.Wrap(err, "instruct the agent to finish")
	}
	return nil
}
