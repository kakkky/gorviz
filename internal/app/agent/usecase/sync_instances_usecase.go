package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type SyncInstancesUsecase struct {
	targets    domain.TargetRegistry
	states     domain.TargetStateStore
	lifecycle  domain.InstanceLifecycle
	kubernetes domain.KubernetesClient
}

func (us *UseCase) NewSyncInstancesUsecase() *SyncInstancesUsecase {
	return &SyncInstancesUsecase{targets: us.targets, states: us.states, lifecycle: us.lifecycle, kubernetes: us.kubernetes}
}

// SyncInstancesUsecaseInput の Self は sidecar 型・ライブラリ型で観測するアプリ。daemon 型では使わず、Kubernetes API から見つける
type SyncInstancesUsecaseInput struct {
	Mode      domain.Mode
	Self      domain.TargetInstance
	PodLogDir string
}

type SyncInstancesUsecaseOutput struct {
	Targets []domain.TargetInstance
	Ended   []observation.Instance
}

func (u *SyncInstancesUsecase) Exec(ctx context.Context, in SyncInstancesUsecaseInput) (SyncInstancesUsecaseOutput, error) {
	found := []domain.TargetInstance{in.Self}
	if in.Mode == domain.ModeDaemon {
		pods, err := u.kubernetes.ListNodePods(ctx)
		if err != nil {
			return SyncInstancesUsecaseOutput{}, err
		}
		found = domain.TargetsFromPods(pods, in.PodLogDir)
	}
	var out SyncInstancesUsecaseOutput
	err := u.targets.UpdateTargets(ctx, func(current []domain.TargetInstance) []domain.TargetInstance {
		out.Targets, out.Ended = domain.SyncTargets(current, found, in.Mode)
		return out.Targets
	})
	if err != nil {
		return SyncInstancesUsecaseOutput{}, err
	}
	if err := u.states.RetainTargetStates(ctx, domain.TargetIDs(out.Targets)); err != nil {
		return SyncInstancesUsecaseOutput{}, err
	}
	for _, e := range out.Ended {
		if err := u.lifecycle.MarkEnded(ctx, e); err != nil {
			return SyncInstancesUsecaseOutput{}, err
		}
	}
	return out, nil
}
