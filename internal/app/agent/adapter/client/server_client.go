package client

import (
	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/socket"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// ServerClient は server への接続にメッセージを書く
type ServerClient struct {
	conn *socket.SocketConn
}

func NewServerClient(conn *socket.SocketConn) (*ServerClient, error) {
	return &ServerClient{conn: conn}, nil
}

func (c *ServerClient) send(kind agent.Kind, v any) error {
	m, err := agent.Encode(kind, v)
	if err != nil {
		return domain.ErrInternal.Wrap(err, "encode message")
	}
	return c.write(m)
}

func (c *ServerClient) write(m agent.Message) error {
	if err := c.conn.Write(m); err != nil {
		return domain.ErrInternal.Wrap(err, "write message")
	}
	return nil
}

func (c *ServerClient) SendHello(h domain.Hello) error {
	return c.send(agent.KindHello, agent.Hello{GoVersion: h.GoVersion, Procs: h.Procs, PID: h.PID})
}

func (c *ServerClient) SendTrace(p []byte) error {
	return c.write(agent.Message{Kind: agent.KindTrace, Payload: p})
}

func (c *ServerClient) SendReply(r domain.Reply) error {
	return c.send(agent.KindReply, agent.Reply{ID: r.ID, Error: r.Error, Coverage: coverageFiles(r.Coverage)})
}

func (c *ServerClient) SendGoroutineLeakProfile(seq uint64, text []byte) error {
	return c.send(agent.KindGoroutineLeakProfile, agent.GoroutineLeakProfile{Seq: seq, Text: text})
}

func (c *ServerClient) SendCoverage(files []domain.CoverageFile) error {
	return c.send(agent.KindCoverage, agent.Coverage{Files: coverageFiles(files)})
}

func (c *ServerClient) SendBye() error {
	return c.write(agent.Message{Kind: agent.KindBye})
}

func (c *ServerClient) BeginFinish() bool { return c.conn.BeginFinish() }

func (c *ServerClient) WaitFinished() { c.conn.WaitFinished() }

func (c *ServerClient) Close() { c.conn.Close() }

func coverageFiles(fs []domain.CoverageFile) []agent.CoverageFile {
	out := make([]agent.CoverageFile, 0, len(fs))
	for _, f := range fs {
		out = append(out, agent.CoverageFile{Name: f.Name, Data: f.Data})
	}
	return out
}
