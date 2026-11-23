package domain

import (
	"cmp"
	"slices"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// Buffered は 1 つのインスタンスについて、送るまでに手元に持っている観測データ。列はどれも積んだ順 (時刻・番号の順)
type Buffered struct {
	Logs        []observation.LogLine
	TraceChunks []observation.TraceChunk
	Snapshots   []observation.GoroutineSnapshot
	LeakChecks  []observation.GoroutineLeakCheck
}

func (b Buffered) Empty() bool {
	return len(b.Logs) == 0 && len(b.TraceChunks) == 0 && len(b.Snapshots) == 0 && len(b.LeakChecks) == 0
}

// BuildBatch は server が持っている位置 (after) の続きを、手元にある分から選ぶ。手元にもう続きがなければ、手元の一番古いものから送る。
// After には server が返した位置をそのまま入れる。agent が先の位置を名乗ると、server は抜けがあるとみなして書かないため
func BuildBatch(in observation.Instance, after observation.Cursor, agent observation.AgentID, buf Buffered, ended bool) observation.Batch {
	b := observation.Batch{Instance: in, After: after, Ended: ended}
	for _, l := range buf.Logs {
		if !LogCovered(after, agent, l) {
			b.Logs = append(b.Logs, l)
		}
	}
	for _, c := range buf.TraceChunks {
		if !TraceChunkCovered(after, c) {
			b.TraceChunks = append(b.TraceChunks, c)
		}
	}
	for _, s := range buf.Snapshots {
		if !SnapshotCovered(after, s) {
			b.Snapshots = append(b.Snapshots, s)
		}
	}
	for _, l := range buf.LeakChecks {
		if !LeakCheckCovered(after, l) {
			b.LeakChecks = append(b.LeakChecks, l)
		}
	}
	slices.SortStableFunc(b.Logs, func(x, y observation.LogLine) int { return cmp.Compare(x.Seq, y.Seq) })
	return b
}

// LogCovered は server がその行をもう持っているか。ログの番号は agent の起動ごとなので、別の起動の位置ならまだ 1 行も持っていない
func LogCovered(c observation.Cursor, agent observation.AgentID, l observation.LogLine) bool {
	return l.Agent == agent && l.Seq <= c.LogSeqFor(agent)
}

func TraceChunkCovered(c observation.Cursor, t observation.TraceChunk) bool {
	return !t.StartedAt.After(c.TraceChunk)
}

func SnapshotCovered(c observation.Cursor, s observation.GoroutineSnapshot) bool {
	return !s.At.After(c.Snapshot)
}

func LeakCheckCovered(c observation.Cursor, l observation.GoroutineLeakCheck) bool {
	return !l.At.After(c.LeakCheck)
}

// Settled は終わったインスタンスを、送り切ったので外してよいか
func Settled(ended bool, rest Buffered) bool {
	return ended && rest.Empty()
}

// ExportOrder は送る対象のインスタンス。手元に観測データがあるものと、終わったことをまだ知らせていないもの
func ExportOrder(buffered, ended []observation.Instance) []observation.Instance {
	seen := map[observation.Instance]bool{}
	var out []observation.Instance
	for _, in := range slices.Concat(buffered, ended) {
		if !seen[in] {
			seen[in] = true
			out = append(out, in)
		}
	}
	slices.SortFunc(out, func(a, b observation.Instance) int {
		return cmp.Or(cmp.Compare(a.Service, b.Service), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Incarnation, b.Incarnation))
	})
	return out
}

// 大きさの目安はメモリで占める量の見積もり。上限を超えたら古いものから捨てる
const (
	stateChangeBytes = 120
	procSpanBytes    = 32
	frameBytes       = 64
	goroutineBytes   = 200
	fixedBytes       = 128
)

func LogLineBytes(l observation.LogLine) int {
	return fixedBytes + len(l.Raw) + len(l.Message)
}

func TraceChunkBytes(c observation.TraceChunk) int {
	n := fixedBytes + len(c.Events)*stateChangeBytes + len(c.Entries)*frameBytes + len(c.Stats.Waits)*frameBytes
	for _, l := range c.Procs {
		n += len(l.Spans) * procSpanBytes
	}
	return n + (len(c.GC)+len(c.Heap)+len(c.GCEnds)+len(c.LogMarks))*procSpanBytes
}

func SnapshotBytes(s observation.GoroutineSnapshot) int {
	n := fixedBytes
	for _, g := range s.Goroutines {
		n += goroutineBytes + len(g.Stack)*frameBytes
	}
	return n
}

func LeakCheckBytes(l observation.GoroutineLeakCheck) int {
	n := fixedBytes
	for _, s := range l.Leaks {
		n += fixedBytes + len(s.Stack)*frameBytes
	}
	return n
}
