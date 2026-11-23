package accessor

import (
	"context"
	"path"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
)

type SnapshotAccessor struct{ root *filesystem.Root }

func NewSnapshotAccessor(root *filesystem.Root) *SnapshotAccessor {
	return &SnapshotAccessor{root: root}
}

func (a *SnapshotAccessor) AppendSnapshots(ctx context.Context, instance observation.Instance, snapshots []observation.GoroutineSnapshot) error {
	err := appendMembers(a.root, snapshots, func(s observation.GoroutineSnapshot) string {
		return instanceFile(instance.Service, instance.Name, domain.DataGoroutineSnapshots, s.At, ".pb.gz")
	}, func(s observation.GoroutineSnapshot) proto.Message { return agentrpc.ToGoroutineSnapshot(s) })
	if err != nil {
		return domain.ErrInternal.Wrap(err, "append goroutine snapshots")
	}
	return nil
}

func (a *SnapshotAccessor) readFile(name string, fn func(observation.GoroutineSnapshot)) error {
	return readMembers(a.root, name, func() *agentrpc.GoroutineSnapshot { return &agentrpc.GoroutineSnapshot{} }, func(m *agentrpc.GoroutineSnapshot) {
		fn(agentrpc.FromGoroutineSnapshot(m))
	})
}

func (a *SnapshotAccessor) ListSnapshots(ctx context.Context, s observation.ServiceName, i observation.InstanceName, from, to time.Time) ([]observation.GoroutineSnapshot, error) {
	var out []observation.GoroutineSnapshot
	for _, p := range periods(domain.DataGoroutineSnapshots, from, to, 0) {
		err := a.readFile(instanceFile(s, i, domain.DataGoroutineSnapshots, p, ".pb.gz"), func(snap observation.GoroutineSnapshot) {
			if !snap.At.Before(from) && !snap.At.After(to) {
				out = append(out, snap)
			}
		})
		if err != nil {
			return nil, domain.ErrInternal.Wrap(err, "read goroutine snapshots")
		}
	}
	return out, nil
}

func (a *SnapshotAccessor) last(s observation.ServiceName, i observation.InstanceName) (time.Time, error) {
	dir := path.Join(instanceDir(s, i), string(domain.DataGoroutineSnapshots))
	entries, err := a.root.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return time.Time{}, err
	}
	var last time.Time
	err = a.readFile(path.Join(dir, entries[len(entries)-1].Name()), func(snap observation.GoroutineSnapshot) { last = maxTime(last, snap.At) })
	return last, err
}

type LeakCheckAccessor struct{ root *filesystem.Root }

func NewLeakCheckAccessor(root *filesystem.Root) *LeakCheckAccessor {
	return &LeakCheckAccessor{root: root}
}

func (a *LeakCheckAccessor) AppendLeakChecks(ctx context.Context, instance observation.Instance, checks []observation.GoroutineLeakCheck) error {
	err := appendMembers(a.root, checks, func(c observation.GoroutineLeakCheck) string {
		return instanceFile(instance.Service, instance.Name, domain.DataGoroutineLeakChecks, c.At, ".pb.gz")
	}, func(c observation.GoroutineLeakCheck) proto.Message { return agentrpc.ToGoroutineLeakCheck(c) })
	if err != nil {
		return domain.ErrInternal.Wrap(err, "append goroutine leak checks")
	}
	return nil
}

func (a *LeakCheckAccessor) readFile(name string, fn func(observation.GoroutineLeakCheck)) error {
	return readMembers(a.root, name, func() *agentrpc.GoroutineLeakCheck { return &agentrpc.GoroutineLeakCheck{} }, func(m *agentrpc.GoroutineLeakCheck) {
		fn(agentrpc.FromGoroutineLeakCheck(m))
	})
}

func (a *LeakCheckAccessor) ListLeakChecks(ctx context.Context, s observation.ServiceName, i observation.InstanceName, from, to time.Time) ([]observation.GoroutineLeakCheck, error) {
	var out []observation.GoroutineLeakCheck
	for _, p := range periods(domain.DataGoroutineLeakChecks, from, to, 0) {
		err := a.readFile(instanceFile(s, i, domain.DataGoroutineLeakChecks, p, ".pb.gz"), func(c observation.GoroutineLeakCheck) {
			if !c.At.Before(from) && !c.At.After(to) {
				out = append(out, c)
			}
		})
		if err != nil {
			return nil, domain.ErrInternal.Wrap(err, "read goroutine leak checks")
		}
	}
	return out, nil
}

func (a *LeakCheckAccessor) last(s observation.ServiceName, i observation.InstanceName) (time.Time, error) {
	dir := path.Join(instanceDir(s, i), string(domain.DataGoroutineLeakChecks))
	entries, err := a.root.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return time.Time{}, err
	}
	var last time.Time
	err = a.readFile(path.Join(dir, entries[len(entries)-1].Name()), func(c observation.GoroutineLeakCheck) { last = maxTime(last, c.At) })
	return last, err
}
