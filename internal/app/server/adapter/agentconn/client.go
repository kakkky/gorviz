// Package agentconn は server の側で、agent 1 つを domain の口として見せる
package agentconn

import (
	"log/slog"
	"net/rpc"
	"slices"
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/agentrpc"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

// Client は server の側で、agent 1 つを domain の口 (TraceRecording・DumpSource・LogSource) として見せる。
// trace は pull の接続で agent に求め、agent が切れている間は「まだない」と答える。agent がつなぎ直したら SetConn で差し替える。
// ログ・dump は push の接続で送られてきたものを手元に持ち、切れている間も最後に受け取ったものを返す
type Client struct {
	mu   sync.RWMutex
	rpc  *rpc.Client
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

func (c *Client) SetConn(rc *rpc.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rpc != nil {
		c.rpc.Close()
	}
	c.rpc = rc
}

func (c *Client) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rpc != nil
}

func (c *Client) call(method string, args, reply any) bool {
	c.mu.RLock()
	rc := c.rpc
	c.mu.RUnlock()
	if rc == nil {
		return false
	}
	if err := rc.Call(agentrpc.AgentServiceName+"."+method, args, reply); err != nil {
		slog.Debug("call agent", slog.String("method", method), slog.Any("err", err))
		if err == rpc.ErrShutdown {
			c.mu.Lock()
			if c.rpc == rc {
				c.rpc = nil
			}
			c.mu.Unlock()
		}
		return false
	}
	return true
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
	c.mu.RLock()
	last := c.last
	c.mu.RUnlock()
	var r agentrpc.ChunkReply
	if !c.call("LatestChunk", agentrpc.ChunkArgs{IfNewerThan: last.StartedAt}, &r) {
		return model.TraceChunk{}, false
	}
	if r.NotModified {
		return last, !last.StartedAt.IsZero()
	}
	if r.OK {
		c.mu.Lock()
		c.last = r.Chunk
		c.mu.Unlock()
	}
	return r.Chunk, r.OK
}

func (c *Client) At(t time.Time) (model.TraceChunk, bool) {
	var r agentrpc.ChunkReply
	return r.Chunk, c.call("ChunkAt", agentrpc.ChunkArgs{At: t}, &r) && r.OK
}

// Between は保存にも使うので、生の trace ごと運ぶ
func (c *Client) Between(from, to time.Time) []model.TraceChunk {
	var r agentrpc.ChunksReply
	c.call("ChunksBetween", agentrpc.BetweenArgs{From: from, To: to, WithRaw: true}, &r)
	return r.Chunks
}

func (c *Client) FindLogMark(seq uint64) (model.TraceChunk, model.TraceLogMark, bool) {
	var r agentrpc.MarkReply
	return r.Chunk, r.Mark, c.call("FindLogMark", agentrpc.SeqArgs{Seq: seq}, &r) && r.OK
}

func (c *Client) Span() (model.RecordingSpan, bool) {
	var r agentrpc.SpanReply
	return r.Span, c.call("Span", agentrpc.Empty{}, &r) && r.OK
}
