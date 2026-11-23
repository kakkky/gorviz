package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// maxReplaySpan は 1 回でつなぐ execution trace の長さの上限。画面で再生する範囲で、重くなりすぎないようにする
const maxReplaySpan = 5 * time.Minute

type ListTraceChunksUsecaseInput struct {
	Service  observation.ServiceName
	Instance observation.InstanceName
	From     time.Time
	To       time.Time
	Now      time.Time
}

// ListTraceChunksUsecaseOutput は Scheduler の再生に使う。Chunk は [From, To] をつないだ 1 つのチャンク (Found が false なら空)。
// FromPreserved は Issue の発生・エラーログの前後に残したものから引いたこと。LatestEnd はインスタンスの最新のチャンクの終わりで、最新を追うときの次の From。
// Leaks は To に一番近いリーク判定を、そのときのスナップショットと突き合わせたもの (LeakCheckAt がその判定の時刻)
type ListTraceChunksUsecaseOutput struct {
	From          time.Time
	To            time.Time
	Found         bool
	FromPreserved bool
	Chunk         observation.TraceChunk
	LatestEnd     time.Time
	Unavailable   []Unavailable
	NearbyTraces  []domain.PreservedTrace
	Leaks         []domain.LeakMatch
	LeakCheckAt   time.Time
}

type ListTraceChunksUsecase struct {
	retention  domain.Retention
	inventory  domain.InventoryAccessor
	holdings   domain.HoldingAccessor
	source     traceSource
	snapshots  domain.SnapshotAccessor
	leakChecks domain.LeakCheckAccessor
}

func (us *UseCase) NewListTraceChunksUsecase() *ListTraceChunksUsecase {
	return &ListTraceChunksUsecase{retention: us.settings.Retention, inventory: us.inventory, holdings: us.holdings, source: traceSource{traces: us.traces, preserved: us.preserved},
		snapshots: us.snapshots, leakChecks: us.leakChecks}
}

func (u *ListTraceChunksUsecase) Exec(ctx context.Context, in ListTraceChunksUsecaseInput) (ListTraceChunksUsecaseOutput, error) {
	if err := requireInstance(ctx, u.inventory, in.Service, in.Instance); err != nil {
		return ListTraceChunksUsecaseOutput{}, err
	}
	h, err := u.holdings.Holding(ctx, in.Service, in.Instance)
	if err != nil {
		return ListTraceChunksUsecaseOutput{}, err
	}
	from, to := in.From, in.To
	if to.IsZero() {
		to = h.TraceEnd
		if to.IsZero() {
			to = in.Now
		}
	}
	if from.IsZero() {
		from = to.Add(-10 * time.Second)
	}
	if !from.Before(to) {
		return ListTraceChunksUsecaseOutput{}, domain.ErrInvalidArgument.With("from must be before to")
	}
	if to.Sub(from) > maxReplaySpan {
		to = from.Add(maxReplaySpan)
	}
	out := ListTraceChunksUsecaseOutput{From: from, To: to, LatestEnd: h.TraceEnd}
	c, preserved, ok, err := u.source.trace(ctx, in.Service, in.Instance, from, to)
	if err != nil {
		return ListTraceChunksUsecaseOutput{}, err
	}
	out.Found, out.FromPreserved, out.Chunk = ok, preserved, c
	if !ok || preserved {
		out.Unavailable = []Unavailable{unavailableOf(u.retention, "executionTrace", domain.DataExecutionTraces, from, in.Now)}
		if out.NearbyTraces, err = u.source.nearbyTraces(ctx, in.Service, in.Instance, from); err != nil {
			return ListTraceChunksUsecaseOutput{}, err
		}
	}
	snapshots, err := u.snapshots.ListSnapshots(ctx, in.Service, in.Instance, to.Add(-snapshotSearch), to.Add(snapshotSearch))
	if err != nil {
		return ListTraceChunksUsecaseOutput{}, err
	}
	if snap, ok := domain.NearestSnapshot(snapshots, to); ok {
		if out.Leaks, out.LeakCheckAt, err = nearestLeakMatches(ctx, u.leakChecks, in.Service, in.Instance, snap.At, snapshots); err != nil {
			return ListTraceChunksUsecaseOutput{}, err
		}
	}
	return out, nil
}
