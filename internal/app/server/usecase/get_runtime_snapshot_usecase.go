package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

type GetRuntimeSnapshotUsecase struct {
	inMemoryKVS domain.InMemoryKVS
}

func (us *UseCase) NewGetRuntimeSnapshotUsecase() *GetRuntimeSnapshotUsecase {
	return &GetRuntimeSnapshotUsecase{inMemoryKVS: us.inMemoryKVS}
}

type GetRuntimeSnapshotUsecaseInput struct{}

type GetRuntimeSnapshotUsecaseOutput struct {
	Snapshot RuntimeSnapshot
	// Lanes は Snapshot.Timeline の P を番号順に並べたもの
	Lanes []ProcLaneSummary
}

type ProcLaneSummary struct {
	Proc        model.ProcID
	Utilization float64
}

// Exec は録画ループが最後に作ったスナップショットを返す。まだ 1 度も観測できていなければ ErrNotFound
func (us *GetRuntimeSnapshotUsecase) Exec(_ context.Context, _ GetRuntimeSnapshotUsecaseInput) (GetRuntimeSnapshotUsecaseOutput, error) {
	v, ok := us.inMemoryKVS.Get(latestSnapshotKey)
	snapshot, isSnapshot := v.(RuntimeSnapshot)
	if !ok || !isSnapshot {
		return GetRuntimeSnapshotUsecaseOutput{}, model.ErrNotFound.With("no snapshot has been taken yet")
	}
	out := GetRuntimeSnapshotUsecaseOutput{Snapshot: snapshot}
	tl := snapshot.Timeline
	for _, lane := range domain.SortedProcs(tl) {
		out.Lanes = append(out.Lanes, ProcLaneSummary{Proc: lane.Proc, Utilization: domain.Utilization(lane, tl.Window)})
	}
	return out, nil
}
