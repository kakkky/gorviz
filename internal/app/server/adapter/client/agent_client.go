package client

import (
	"context"
	"errors"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/socket"
	"github.com/kakkky/gorviz/internal/protocol/agent"
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

func (c *AgentClient) request(ctx context.Context, in agent.Instruction) (agent.Reply, error) {
	r, err := c.conn.Request(ctx, in)
	if errors.Is(err, socket.ErrNotConnected) {
		return agent.Reply{}, domain.ErrInvalidArgument.Wrap(err, "the app is not running with the agent")
	}
	if err != nil {
		return agent.Reply{}, domain.ErrInternal.Wrap(err, "instruct the agent "+string(in.Op))
	}
	if r.Error != "" {
		return agent.Reply{}, domain.ErrInternal.With("the agent failed to " + string(in.Op) + ": " + r.Error)
	}
	return r, nil
}

func (c *AgentClient) WriteTraceEvent(ctx context.Context, event string) error {
	_, err := c.request(ctx, agent.Instruction{Op: agent.OpWriteTraceEvent, Event: event})
	return err
}

func (c *AgentClient) ResetCoverage(ctx context.Context) error {
	_, err := c.request(ctx, agent.Instruction{Op: agent.OpResetCoverage})
	return err
}

func (c *AgentClient) ReadCoverage(ctx context.Context) ([]domain.CoverageFile, error) {
	r, err := c.request(ctx, agent.Instruction{Op: agent.OpWriteCoverage})
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
	_, err := c.request(ctx, agent.Instruction{Op: agent.OpWriteGoroutineLeak})
	return err
}

func (c *AgentClient) RequestThreadCount(ctx context.Context) error {
	_, err := c.request(ctx, agent.Instruction{Op: agent.OpWriteThreadCount})
	return err
}

// Finish は返事を待たない。agent は最後の観測データと終わりの合図を送ってから接続を閉じる
func (c *AgentClient) Finish(_ context.Context) error {
	if err := c.conn.Send(agent.Instruction{Op: agent.OpFinish}); err != nil && !errors.Is(err, socket.ErrNotConnected) {
		return domain.ErrInternal.Wrap(err, "instruct the agent to finish")
	}
	return nil
}
