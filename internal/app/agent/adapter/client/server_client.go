package client

import (
	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/socket"
	"github.com/kakkky/gorviz/internal/protocol/agentsocket"
)

// ServerClient は server への接続にメッセージを書く
type ServerClient struct {
	conn *socket.SocketConn
}

func NewServerClient(conn *socket.SocketConn) (*ServerClient, error) {
	return &ServerClient{conn: conn}, nil
}

func (c *ServerClient) send(kind agentsocket.Kind, v any) error {
	m, err := agentsocket.Encode(kind, v)
	if err != nil {
		return domain.ErrInternal.Wrap(err, "encode message")
	}
	return c.write(m)
}

func (c *ServerClient) write(m agentsocket.Message) error {
	if err := c.conn.Write(m); err != nil {
		return domain.ErrInternal.Wrap(err, "write message")
	}
	return nil
}

func (c *ServerClient) SendHello(h domain.Hello) error {
	return c.send(agentsocket.KindHello, agentsocket.Hello{GoVersion: h.GoVersion, Procs: h.Procs, PID: h.PID})
}

func (c *ServerClient) SendTrace(p []byte) error {
	return c.write(agentsocket.Message{Kind: agentsocket.KindTrace, Payload: p})
}

func (c *ServerClient) SendReply(r domain.Reply) error {
	return c.send(agentsocket.KindReply, agentsocket.Reply{ID: r.ID, Error: r.Error, Coverage: coverageFiles(r.Coverage)})
}

func (c *ServerClient) SendGoroutineLeakProfile(seq uint64, text []byte) error {
	return c.send(agentsocket.KindGoroutineLeakProfile, agentsocket.GoroutineLeakProfile{Seq: seq, Text: text})
}

func (c *ServerClient) SendCoverage(files []domain.CoverageFile) error {
	return c.send(agentsocket.KindCoverage, agentsocket.Coverage{Files: coverageFiles(files)})
}

func (c *ServerClient) SendBye() error {
	return c.write(agentsocket.Message{Kind: agentsocket.KindBye})
}

func (c *ServerClient) BeginFinish() bool { return c.conn.BeginFinish() }

func (c *ServerClient) WaitFinished() { c.conn.WaitFinished() }

func (c *ServerClient) Close() { c.conn.Close() }

func coverageFiles(fs []domain.CoverageFile) []agentsocket.CoverageFile {
	out := make([]agentsocket.CoverageFile, 0, len(fs))
	for _, f := range fs {
		out = append(out, agentsocket.CoverageFile{Name: f.Name, Data: f.Data})
	}
	return out
}
