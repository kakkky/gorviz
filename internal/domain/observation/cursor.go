package observation

import "time"

// Cursor はインスタンスごとに、観測データをどこまで持っているか。ログは agent の起動ごとの番号、それ以外は最後に持っているものの時刻
type Cursor struct {
	LogAgent   AgentID
	LogSeq     uint64
	TraceChunk time.Time
	Snapshot   time.Time
	LeakCheck  time.Time
}

// LogSeqFor は agent が別の起動なら、その agent のログはまだ 1 行も持っていない
func (c Cursor) LogSeqFor(agent AgentID) uint64 {
	if c.LogAgent != agent {
		return 0
	}
	return c.LogSeq
}

// Batch は 1 つのインスタンスの、1 回で送る観測データ。After は agent が「server はここまで持っている」と思っている位置で、
// 各列は After の続きから時刻 (ログは番号) の順に並ぶ。Ended はインスタンスが終わって、これが最後の分であること
type Batch struct {
	Instance    Instance
	After       Cursor
	Logs        []LogLine
	TraceChunks []TraceChunk
	Snapshots   []GoroutineSnapshot
	LeakChecks  []GoroutineLeakCheck
	Ended       bool
}

func (b Batch) Empty() bool {
	return len(b.Logs) == 0 && len(b.TraceChunks) == 0 && len(b.Snapshots) == 0 && len(b.LeakChecks) == 0 && !b.Ended
}
