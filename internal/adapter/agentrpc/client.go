package agentrpc

import (
	"log/slog"
	"net/rpc"
	"slices"
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/domain"
)

// Client は server の側で、agent 1 つを domain の口 (TraceRecording・DumpSource・LogSource) として見せる。
// trace は pull の接続で agent に求め、agent が切れている間は「まだない」と答える。agent がつなぎ直したら SetConn で差し替える。
// ログ・dump は push の接続で送られてきたものを手元に持ち、切れている間も最後に受け取ったものを返す
type Client struct {
	mu   sync.RWMutex
	rpc  *rpc.Client
	last domain.TraceChunk

	pushed sync.Mutex
	// logs は送られてきて、まだ LogsSince で読まれていないログ。maxLogs を超えたら古いものから捨てる
	logs      []domain.LogLine
	maxLogs   int
	lastLogID domain.LogLineID
	dump      domain.GoroutineDump
	hasDump   bool
}

func NewClient(maxLogs int) *Client {
	return &Client{maxLogs: maxLogs}
}

func (c *Client) receive(a PushArgs) {
	c.pushed.Lock()
	defer c.pushed.Unlock()
	for _, l := range a.Logs {
		if l.ID <= c.lastLogID {
			continue
		}
		c.logs = append(c.logs, l)
		c.lastLogID = l.ID
	}
	if over := len(c.logs) - c.maxLogs; over > 0 {
		c.logs = c.logs[over:]
	}
	if a.Dump != nil {
		c.dump, c.hasDump = *a.Dump, true
	}
}

// ingest は push の接続の上で agent からの呼び出しを受け、Client に渡す
type ingest struct{ c *Client }

func (i *ingest) Push(a PushArgs, _ *Empty) error {
	i.c.receive(a)
	return nil
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
	if err := rc.Call(agentServiceName+"."+method, args, reply); err != nil {
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

func (c *Client) LatestDump() (domain.GoroutineDump, bool) {
	c.pushed.Lock()
	defer c.pushed.Unlock()
	return c.dump, c.hasDump
}

// LogsSince は after までは読み終えたものとして手元から捨て、その後ろを返す
func (c *Client) LogsSince(after domain.LogLineID, limit int) []domain.LogLine {
	c.pushed.Lock()
	defer c.pushed.Unlock()
	read := 0
	for read < len(c.logs) && c.logs[read].ID <= after {
		read++
	}
	c.logs = c.logs[read:]
	return slices.Clone(c.logs[:min(limit, len(c.logs))])
}

func (c *Client) Append(domain.TraceChunk) {}

// Latest は画面の更新ごとに呼ばれるので、前と同じ区切りなら運ばずに手元のものを返す
func (c *Client) Latest() (domain.TraceChunk, bool) {
	c.mu.RLock()
	last := c.last
	c.mu.RUnlock()
	var r ChunkReply
	if !c.call("LatestChunk", ChunkArgs{IfNewerThan: last.StartedAt}, &r) {
		return domain.TraceChunk{}, false
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

func (c *Client) At(t time.Time) (domain.TraceChunk, bool) {
	var r ChunkReply
	return r.Chunk, c.call("ChunkAt", ChunkArgs{At: t}, &r) && r.OK
}

// Between は保存にも使うので、生の trace ごと運ぶ
func (c *Client) Between(from, to time.Time) []domain.TraceChunk {
	var r ChunksReply
	c.call("ChunksBetween", BetweenArgs{From: from, To: to, WithRaw: true}, &r)
	return r.Chunks
}

func (c *Client) FindLogMark(seq uint64) (domain.TraceChunk, domain.TraceLogMark, bool) {
	var r MarkReply
	return r.Chunk, r.Mark, c.call("FindLogMark", SeqArgs{Seq: seq}, &r) && r.OK
}

func (c *Client) Span() (domain.RecordingSpan, bool) {
	var r SpanReply
	return r.Span, c.call("Span", Empty{}, &r) && r.OK
}
