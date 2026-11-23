package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// snapshotSearch は、ある時刻に近いスナップショットを探す前後の幅
const snapshotSearch = time.Hour

type ListGoroutinesAtUsecaseInput struct {
	Service  observation.ServiceName
	Instance observation.InstanceName
	At       time.Time
	Cursor   domain.PageCursor
	MaxBytes int
	Now      time.Time
}

// ListGoroutinesAtUsecaseOutput の SnapshotAt は使ったスナップショットの時刻。LeakCheckAt はリークの数を突き合わせたリーク判定の時刻
type ListGoroutinesAtUsecaseOutput struct {
	At            time.Time
	Found         bool
	SnapshotAt    time.Time
	Total         int
	Groups        []domain.SnapshotGroup
	Next          domain.PageCursor
	LeakCheckAt   time.Time
	RetainedSince time.Time
}

type ListGoroutinesAtUsecase struct {
	retention  domain.Retention
	inventory  domain.InventoryAccessor
	snapshots  domain.SnapshotAccessor
	leakChecks domain.LeakCheckAccessor
}

func (us *UseCase) NewListGoroutinesAtUsecase() *ListGoroutinesAtUsecase {
	return &ListGoroutinesAtUsecase{retention: us.settings.Retention, inventory: us.inventory, snapshots: us.snapshots, leakChecks: us.leakChecks}
}

func (u *ListGoroutinesAtUsecase) Exec(ctx context.Context, in ListGoroutinesAtUsecaseInput) (ListGoroutinesAtUsecaseOutput, error) {
	if in.At.IsZero() {
		return ListGoroutinesAtUsecaseOutput{}, domain.ErrInvalidArgument.With("at is required")
	}
	if err := requireInstance(ctx, u.inventory, in.Service, in.Instance); err != nil {
		return ListGoroutinesAtUsecaseOutput{}, err
	}
	after, err := domain.ParsePageCursor(in.Cursor)
	if err != nil {
		return ListGoroutinesAtUsecaseOutput{}, err
	}
	out := ListGoroutinesAtUsecaseOutput{At: in.At, Groups: []domain.SnapshotGroup{}, RetainedSince: u.retention.RetainedSince(domain.DataGoroutineSnapshots, in.Now)}
	snapshots, err := u.snapshots.ListSnapshots(ctx, in.Service, in.Instance, in.At.Add(-snapshotSearch), in.At.Add(snapshotSearch))
	if err != nil {
		return ListGoroutinesAtUsecaseOutput{}, err
	}
	snap, ok := domain.NearestSnapshot(snapshots, in.At)
	if !ok {
		return out, nil
	}
	matches, leakCheckAt, err := nearestLeakMatches(ctx, u.leakChecks, in.Service, in.Instance, snap.At, snapshots)
	if err != nil {
		return ListGoroutinesAtUsecaseOutput{}, err
	}
	out.LeakCheckAt = leakCheckAt
	out.Found, out.SnapshotAt, out.Total = true, snap.At, len(snap.Goroutines)
	groups := domain.GroupSnapshot(snap, matches)
	out.Groups, out.Next = domain.Page(groups, after, in.MaxBytes, func(g domain.SnapshotGroup) int {
		return len(g.Creator) + len(g.CreatorLocation) + len(g.Function) + len(g.Location) + len(g.WaitReason) + 8*len(g.Goroutines) + 96
	})
	if out.Groups == nil {
		out.Groups = []domain.SnapshotGroup{}
	}
	return out, nil
}

// nearestLeakMatches は at に一番近いリーク判定を、その判定に一番近いスナップショットと突き合わせる。判定がなければ空
func nearestLeakMatches(ctx context.Context, leakChecks domain.LeakCheckAccessor, s observation.ServiceName, i observation.InstanceName, at time.Time, snapshots []observation.GoroutineSnapshot) ([]domain.LeakMatch, time.Time, error) {
	checks, err := leakChecks.ListLeakChecks(ctx, s, i, at.Add(-leakLookback), at.Add(leakLookback))
	if err != nil {
		return nil, time.Time{}, err
	}
	var best observation.GoroutineLeakCheck
	for _, c := range checks {
		if c.Status == observation.LeakCheckDone && (best.At.IsZero() || c.At.Sub(at).Abs() < best.At.Sub(at).Abs()) {
			best = c
		}
	}
	if best.At.IsZero() {
		return nil, time.Time{}, nil
	}
	near, _ := domain.NearestSnapshot(snapshots, best.At)
	return domain.MatchLeaks(best, near), best.At, nil
}
