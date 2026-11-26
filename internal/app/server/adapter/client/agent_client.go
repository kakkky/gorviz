package client

import (
	"context"
	"errors"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/socket"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// AgentClient は agent に要求を送り、応答を受け取る
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

func (c *AgentClient) request(ctx context.Context, in agent.Request) (agent.Response, error) {
	r, err := c.conn.Request(ctx, in)
	if errors.Is(err, socket.ErrNotConnected) {
		return agent.Response{}, domain.ErrInvalidArgument.Wrap(err, "the app is not running with the agent")
	}
	if err != nil {
		return agent.Response{}, domain.ErrInternal.Wrap(err, "instruct the agent "+string(in.Method))
	}
	if r.Error != "" {
		return agent.Response{}, domain.ErrInternal.With("the agent failed to " + string(in.Method) + ": " + r.Error)
	}
	return r, nil
}

func (c *AgentClient) WriteTraceEvent(ctx context.Context, event string) error {
	_, err := c.request(ctx, agent.Request{Method: agent.MethodWriteTraceEvent, Event: event})
	return err
}

func (c *AgentClient) ResetCoverage(ctx context.Context) error {
	_, err := c.request(ctx, agent.Request{Method: agent.MethodResetCoverage})
	return err
}

func (c *AgentClient) ReadCoverage(ctx context.Context) ([]domain.CoverageFile, error) {
	r, err := c.request(ctx, agent.Request{Method: agent.MethodWriteCoverage})
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
	_, err := c.request(ctx, agent.Request{Method: agent.MethodWriteGoroutineLeak})
	return err
}

func (c *AgentClient) RequestThreadCount(ctx context.Context) error {
	_, err := c.request(ctx, agent.Request{Method: agent.MethodWriteThreadCount})
	return err
}

// Finish は応答を待たない。agent は最後の観測データと終わりの合図を送ってから接続を閉じる
func (c *AgentClient) Finish(_ context.Context) error {
	if err := c.conn.Send(agent.Request{Method: agent.MethodFinish}); err != nil && !errors.Is(err, socket.ErrNotConnected) {
		return domain.ErrInternal.Wrap(err, "instruct the agent to finish")
	}
	return nil
}
