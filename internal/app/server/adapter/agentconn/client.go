// Package agentconn は server の側で、agent 1 つを domain の口として見せる
package agentconn

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/kakkky/gorviz/internal/app/api/agentrpc"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

// Client は server の側で、agent 1 つを domain の口 (TraceRecording・DumpSource・LogSource) として見せる。
// trace は pull の接続で agent に gRPC で求め、agent が切れている間は「まだない」と答える。agent がつなぎ直したら SetConn で差し替える。
// ログ・dump は push の接続で送られてきたものを手元に持ち、切れている間も最後に受け取ったものを返す
type Client struct {
	mu   sync.RWMutex
	cc   *grpc.ClientConn
	last model.TraceChunk

	pushed sync.Mutex
	// logs は送られてきて、まだ LogsSince で読まれていないログ。maxLogs を超えたら古いものから捨てる
	logs      []model.LogLine
	maxLogs   int
	lastLogID model.LogLineID
	dump      model.GoroutineDump
	hasDump   bool
}

func NewClient(maxLogs int) *Client {
	return &Client{maxLogs: maxLogs}
}

// Receive は push の接続で agent から送られてきたものを手元に持つ (domain.AgentInbox)
func (c *Client) Receive(logs []model.LogLine, dump *model.GoroutineDump) {
	c.pushed.Lock()
	defer c.pushed.Unlock()
	for _, l := range logs {
		if l.ID <= c.lastLogID {
			continue
		}
		c.logs = append(c.logs, l)
		c.lastLogID = l.ID
	}
	if over := len(c.logs) - c.maxLogs; over > 0 {
		c.logs = c.logs[over:]
	}
	if dump != nil {
		c.dump, c.hasDump = *dump, true
	}
}

func (c *Client) SetConn(cc *grpc.ClientConn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cc != nil {
		c.cc.Close()
	}
	c.cc = cc
}

func (c *Client) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cc != nil
}

// agent は今の pull の接続で agent を呼ぶクライアントを返す。呼び出しが失敗したら done に渡す
func (c *Client) agent() (agentrpc.AgentClient, func(method string, err error), bool) {
	c.mu.RLock()
	cc := c.cc
	c.mu.RUnlock()
	if cc == nil {
		return nil, nil, false
	}
	done := func(method string, err error) {
		slog.Debug("call agent", slog.String("method", method), slog.Any("err", err))
		// 接続が切れると gRPC はつなぎ直さないので、agent がつなぎ直してくる (SetConn) まで「つながっていない」とみなす
		if status.Code(err) == codes.Unavailable {
			c.mu.Lock()
			if c.cc == cc {
				c.cc.Close()
				c.cc = nil
			}
			c.mu.Unlock()
		}
	}
	return agentrpc.NewAgentClient(cc), done, true
}

func (c *Client) LatestDump() (model.GoroutineDump, bool) {
	c.pushed.Lock()
	defer c.pushed.Unlock()
	return c.dump, c.hasDump
}

// LogsSince は after までは読み終えたものとして手元から捨て、その後ろを返す
func (c *Client) LogsSince(after model.LogLineID, limit int) []model.LogLine {
	c.pushed.Lock()
	defer c.pushed.Unlock()
	read := 0
	for read < len(c.logs) && c.logs[read].ID <= after {
		read++
	}
	c.logs = c.logs[read:]
	return slices.Clone(c.logs[:min(limit, len(c.logs))])
}

func (c *Client) Append(model.TraceChunk) {}

// Latest は画面の更新ごとに呼ばれるので、前と同じ区切りなら運ばずに手元のものを返す
func (c *Client) Latest() (model.TraceChunk, bool) {
	agent, fail, ok := c.agent()
	if !ok {
		return model.TraceChunk{}, false
	}
	c.mu.RLock()
	last := c.last
	c.mu.RUnlock()
	r, err := agent.LatestChunk(context.Background(), &agentrpc.LatestChunkRequest{IfNewerThan: agentrpc.Timestamp(last.StartedAt)})
	if err != nil {
		fail("LatestChunk", err)
		return model.TraceChunk{}, false
	}
	if r.GetNotModified() {
		return last, !last.StartedAt.IsZero()
	}
	if r.GetChunk() == nil {
		return model.TraceChunk{}, false
	}
	chunk := r.GetChunk().Model()
	c.mu.Lock()
	c.last = chunk
	c.mu.Unlock()
	return chunk, true
}

func (c *Client) At(t time.Time) (model.TraceChunk, bool) {
	agent, fail, ok := c.agent()
	if !ok {
		return model.TraceChunk{}, false
	}
	r, err := agent.ChunkAt(context.Background(), &agentrpc.ChunkAtRequest{At: agentrpc.Timestamp(t)})
	if err != nil {
		fail("ChunkAt", err)
		return model.TraceChunk{}, false
	}
	return r.GetChunk().Model(), r.GetChunk() != nil
}

// Between は保存にも使うので、生の trace ごと運ぶ。途中で失敗したら何も返さない
func (c *Client) Between(from, to time.Time) []model.TraceChunk {
	agent, fail, ok := c.agent()
	if !ok {
		return nil
	}
	stream, err := agent.ChunksBetween(context.Background(), &agentrpc.ChunksBetweenRequest{From: agentrpc.Timestamp(from), To: agentrpc.Timestamp(to)})
	if err != nil {
		fail("ChunksBetween", err)
		return nil
	}
	var out []model.TraceChunk
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			fail("ChunksBetween", err)
			return nil
		}
		out = append(out, chunk.Model())
	}
}

func (c *Client) FindLogMark(seq uint64) (model.TraceChunk, model.TraceLogMark, bool) {
	agent, fail, ok := c.agent()
	if !ok {
		return model.TraceChunk{}, model.TraceLogMark{}, false
	}
	r, err := agent.FindLogMark(context.Background(), &agentrpc.FindLogMarkRequest{Seq: seq})
	if err != nil {
		fail("FindLogMark", err)
		return model.TraceChunk{}, model.TraceLogMark{}, false
	}
	return r.GetChunk().Model(), r.GetMark().Model(), r.GetChunk() != nil
}

func (c *Client) Span() (model.RecordingSpan, bool) {
	agent, fail, ok := c.agent()
	if !ok {
		return model.RecordingSpan{}, false
	}
	r, err := agent.Span(context.Background(), &emptypb.Empty{})
	if err != nil {
		fail("Span", err)
		return model.RecordingSpan{}, false
	}
	return r.GetSpan().Model(), r.GetSpan() != nil
}
