package agentrpc

import (
	"log/slog"
	"net/rpc"
	"sync"
	"time"

	"github.com/kakkky/gorviz/domain"
)

// Client は server の側で、agent 1 つを domain の口 (TraceRecording・DumpSource・LogSource・SchedTraceSource) として見せる。
// agent が切れている間は、どの口も「まだない」と答える。agent がつなぎ直したら SetConn で差し替える
type Client struct {
	mu   sync.RWMutex
	rpc  *rpc.Client
	last domain.TraceChunk
}

func NewClient() *Client {
	return &Client{}
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
	if err := rc.Call(serviceName+"."+method, args, reply); err != nil {
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
	var r DumpReply
	return r.Dump, c.call("LatestDump", Empty{}, &r) && r.OK
}

func (c *Client) LogsSince(after domain.LogLineID, limit int) []domain.LogLine {
	var r LogsReply
	c.call("LogsSince", LogsArgs{After: after, Limit: limit}, &r)
	return r.Lines
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

func (c *Client) SchedTraceLatest() (domain.SchedTraceReport, bool) {
	var r SchedTraceReply
	return r.Report, c.call("SchedTrace", Empty{}, &r) && r.OK
}

// SchedTrace は domain.SchedTraceSource として見せる
type SchedTrace struct{ c *Client }

func (c *Client) SchedTrace() SchedTrace { return SchedTrace{c} }

func (s SchedTrace) Latest() (domain.SchedTraceReport, bool) { return s.c.SchedTraceLatest() }
