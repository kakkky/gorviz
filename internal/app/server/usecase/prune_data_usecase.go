package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type PruneDataUsecaseInput struct {
	Now time.Time
}

type PruneDataUsecaseOutput struct {
	Expired      int
	OverCapacity int
	RemovedBytes int64
}

type PruneDataUsecase struct {
	retention    domain.Retention
	maxDiskBytes int64
	inventory    domain.InventoryAccessor
	dataFiles    domain.DataFileAccessor
	checkpoints  domain.CheckpointAccessor
}

func (us *UseCase) NewPruneDataUsecase() *PruneDataUsecase {
	return &PruneDataUsecase{retention: us.settings.Retention, maxDiskBytes: us.settings.MaxDiskBytes, inventory: us.inventory, dataFiles: us.dataFiles, checkpoints: us.checkpoints}
}

func (u *PruneDataUsecase) Exec(ctx context.Context, in PruneDataUsecaseInput) (PruneDataUsecaseOutput, error) {
	files, err := u.dataFiles.ListDataFiles(ctx)
	if err != nil {
		return PruneDataUsecaseOutput{}, err
	}
	expired := u.retention.ExpiredFiles(files, in.Now)
	removed := map[string]bool{}
	for _, f := range expired {
		removed[f.Path] = true
	}
	var rest []domain.DataFile
	for _, f := range files {
		if !removed[f.Path] {
			rest = append(rest, f)
		}
	}
	over := domain.OverCapacity(rest, u.maxDiskBytes, in.Now)
	out := PruneDataUsecaseOutput{Expired: len(expired), OverCapacity: len(over)}
	for _, f := range append(expired, over...) {
		out.RemovedBytes += f.Size
	}
	if err := u.dataFiles.RemoveDataFiles(ctx, append(expired, over...)); err != nil {
		return PruneDataUsecaseOutput{}, err
	}
	if err := u.pruneCheckpoints(ctx, in.Now); err != nil {
		return PruneDataUsecaseOutput{}, err
	}
	return out, nil
}

func (u *PruneDataUsecase) pruneCheckpoints(ctx context.Context, now time.Time) error {
	services, err := u.inventory.ListServices(ctx)
	if err != nil {
		return err
	}
	for _, s := range services {
		instances, err := u.inventory.ListInstances(ctx, s)
		if err != nil {
			return err
		}
		live := map[observation.InstanceName]bool{}
		for _, i := range instances {
			live[i] = true
		}
		err = u.checkpoints.ModifyCheckpoints(ctx, s, func(cps domain.Checkpoints) domain.Checkpoints {
			return cps.Prune(live, u.retention, now)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
