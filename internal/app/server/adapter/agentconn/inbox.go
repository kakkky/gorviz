// Package agentconn は server の側で、agent 1 つから届いたものを domain の口として見せる
package agentconn

import (
	"slices"
	"sync"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// Inbox は agent から送られてきたログ・dump を手元に持ち、domain の口 (AgentInbox・DumpSource・LogSource) として見せる。
// agent が切れている間も、最後に受け取ったものを返す
type Inbox struct {
	mu sync.Mutex
	// logs は送られてきて、まだ LogsSince で読まれていないログ。maxLogs を超えたら古いものから捨てる
	logs      []model.LogLine
	maxLogs   int
	lastLogID model.LogLineID
	dump      model.GoroutineDump
	hasDump   bool
	// conns は今つながっている接続の数。つなぎ直しでは古い接続が閉じる前に新しい接続が来ることがあるので数で持つ
	conns int
}

func NewInbox(maxLogs int) *Inbox {
	return &Inbox{maxLogs: maxLogs}
}

func (c *Inbox) Receive(logs []model.LogLine, dump *model.GoroutineDump) {
	c.mu.Lock()
	defer c.mu.Unlock()
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

// Attach は接続が張られたときに呼び、返した関数を接続が切れたときに呼ぶ
func (c *Inbox) Attach() (detach func()) {
	c.mu.Lock()
	c.conns++
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.conns--
			c.mu.Unlock()
		})
	}
}

func (c *Inbox) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conns > 0
}

func (c *Inbox) LatestDump() (model.GoroutineDump, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dump, c.hasDump
}

// LogsSince は after までは読み終えたものとして手元から捨て、その後ろを返す
func (c *Inbox) LogsSince(after model.LogLineID, limit int) []model.LogLine {
	c.mu.Lock()
	defer c.mu.Unlock()
	read := 0
	for read < len(c.logs) && c.logs[read].ID <= after {
		read++
	}
	c.logs = c.logs[read:]
	return slices.Clone(c.logs[:min(limit, len(c.logs))])
}
