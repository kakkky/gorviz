package accessor

import (
	"context"
	"path"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// logScanHours は、インスタンスの最後のログを探すときにさかのぼる時間帯の数。ログはサービスのファイルにまとめて書くので、ほかのインスタンスの行を読み飛ばすことになるため
const logScanHours = 48

// HoldingAccessor はインスタンスの観測データをどこまで持っているかを、ディスクに書いてある最後の分から引く。
// 毎回ディスクを読むと重いので、プロセスで一度引いたら、書くたびにメモリの値を進める
type HoldingAccessor struct {
	root       *filesystem.Root
	kvs        *kvs.InMemoryKVS
	traces     *TraceAccessor
	snapshots  *SnapshotAccessor
	leakChecks *LeakCheckAccessor
}

func NewHoldingAccessor(root *filesystem.Root, store *kvs.InMemoryKVS) *HoldingAccessor {
	return &HoldingAccessor{root: root, kvs: store, traces: NewTraceAccessor(root), snapshots: NewSnapshotAccessor(root), leakChecks: NewLeakCheckAccessor(root)}
}

func holdingKey(s observation.ServiceName, i observation.InstanceName) string {
	return "holding\x00" + string(s) + "\x00" + string(i)
}

func (a *HoldingAccessor) LockInstance(s observation.ServiceName, i observation.InstanceName) func() {
	return a.root.Lock(instanceDir(s, i))
}

func (a *HoldingAccessor) RememberHolding(ctx context.Context, s observation.ServiceName, i observation.InstanceName, h domain.Holding) {
	a.kvs.Set(holdingKey(s, i), h)
}

func (a *HoldingAccessor) ForgetHolding(ctx context.Context, s observation.ServiceName, i observation.InstanceName) {
	a.kvs.Delete(holdingKey(s, i))
}

func (a *HoldingAccessor) Holding(ctx context.Context, s observation.ServiceName, i observation.InstanceName) (domain.Holding, error) {
	if v, ok := a.kvs.Get(holdingKey(s, i)); ok {
		return v.(domain.Holding), nil
	}
	h, err := a.fromDisk(s, i)
	if err != nil {
		return domain.Holding{}, domain.ErrInternal.Wrap(err, "read what is held")
	}
	a.kvs.Set(holdingKey(s, i), h)
	return h, nil
}

func (a *HoldingAccessor) fromDisk(s observation.ServiceName, i observation.InstanceName) (domain.Holding, error) {
	var h domain.Holding
	e, ok, err := a.traces.lastEntry(s, i)
	if err != nil {
		return h, err
	}
	if ok {
		h.Cursor.TraceChunk, h.TraceEnd = e.Start, e.End
	}
	if h.Cursor.Snapshot, err = a.snapshots.last(s, i); err != nil {
		return h, err
	}
	if h.Cursor.LeakCheck, err = a.leakChecks.last(s, i); err != nil {
		return h, err
	}
	last, ok, err := a.lastLog(s, i)
	if err != nil {
		return h, err
	}
	if ok {
		h.Cursor.LogAgent, h.Cursor.LogSeq = last.Agent, last.Seq
	}
	h.Latest = maxTime(maxTime(maxTime(h.TraceEnd, h.Cursor.Snapshot), h.Cursor.LeakCheck), last.At)
	return h, nil
}

// lastLog はインスタンスが最後に書いたログ。agent はその行の agent、番号はその agent の最大
func (a *HoldingAccessor) lastLog(s observation.ServiceName, i observation.InstanceName) (domain.StoredLog, bool, error) {
	dir := path.Join(serviceDir(s), string(domain.DataLogs))
	entries, err := a.root.ReadDir(dir)
	if err != nil {
		return domain.StoredLog{}, false, err
	}
	for j, scanned := len(entries)-1, 0; j >= 0 && scanned < logScanHours; j, scanned = j-1, scanned+1 {
		var (
			last domain.StoredLog
			ok   bool
		)
		err := eachLine(a.root, path.Join(dir, entries[j].Name()), func(l domain.StoredLog) {
			if l.Instance != i {
				return
			}
			if !ok || l.Agent != last.Agent || l.Seq > last.Seq {
				last, ok = l, true
			}
		})
		if err != nil {
			return domain.StoredLog{}, false, err
		}
		if ok {
			return last, true, nil
		}
	}
	return domain.StoredLog{}, false, nil
}
