package accessor

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type CheckpointAccessor struct{ root *filesystem.Root }

func NewCheckpointAccessor(root *filesystem.Root) *CheckpointAccessor {
	return &CheckpointAccessor{root: root}
}

func (a *CheckpointAccessor) read(s observation.ServiceName) (domain.Checkpoints, error) {
	cps := domain.Checkpoints{}
	if _, err := readChecked(a.root, checkpointFile(s), &cps); err != nil {
		return nil, err
	}
	return cps, nil
}

func (a *CheckpointAccessor) ReadCheckpoints(ctx context.Context, s observation.ServiceName) (domain.Checkpoints, error) {
	cps, err := a.read(s)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read checkpoints")
	}
	return cps, nil
}

func (a *CheckpointAccessor) write(s observation.ServiceName, cps domain.Checkpoints) error {
	b, err := checkedLine(cps)
	if err != nil {
		return err
	}
	return a.root.Replace(checkpointFile(s), b)
}

func (a *CheckpointAccessor) UpdateCheckpoint(ctx context.Context, s observation.ServiceName, p domain.Process, key string, cp domain.Checkpoint) error {
	unlock := a.root.Lock(checkpointFile(s))
	defer unlock()
	cps, err := a.read(s)
	if err != nil {
		return domain.ErrInternal.Wrap(err, "read checkpoints")
	}
	if cps[p] == nil {
		cps[p] = map[string]domain.Checkpoint{}
	}
	cps[p][key] = cp
	if err := a.write(s, cps); err != nil {
		return domain.ErrInternal.Wrap(err, "write checkpoints")
	}
	return nil
}

func (a *CheckpointAccessor) ModifyCheckpoints(ctx context.Context, s observation.ServiceName, modify func(domain.Checkpoints) domain.Checkpoints) error {
	unlock := a.root.Lock(checkpointFile(s))
	defer unlock()
	cps, err := a.read(s)
	if err != nil {
		return domain.ErrInternal.Wrap(err, "read checkpoints")
	}
	if len(cps) == 0 {
		return nil
	}
	next := modify(cps)
	if len(next) == 0 {
		if err := a.root.RemoveAll(checkpointFile(s), checkpointsDir); err != nil {
			return domain.ErrInternal.Wrap(err, "remove checkpoints")
		}
		return nil
	}
	if err := a.write(s, next); err != nil {
		return domain.ErrInternal.Wrap(err, "write checkpoints")
	}
	return nil
}
