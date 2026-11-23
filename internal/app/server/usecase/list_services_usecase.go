package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type ListServicesUsecaseInput struct {
	Now time.Time
}

type InstanceSummary struct {
	Name         observation.InstanceName
	Incarnations []domain.Incarnation
	Running      bool
}

type ServiceSummary struct {
	Name      observation.ServiceName
	Instances []InstanceSummary
}

type ListServicesUsecaseOutput struct {
	Services []ServiceSummary
}

type ListServicesUsecase struct {
	retention domain.Retention
	inventory domain.InventoryAccessor
	lifecycle domain.LifecycleAccessor
}

func (us *UseCase) NewListServicesUsecase() *ListServicesUsecase {
	return &ListServicesUsecase{retention: us.settings.Retention, inventory: us.inventory, lifecycle: us.lifecycle}
}

func (u *ListServicesUsecase) Exec(ctx context.Context, in ListServicesUsecaseInput) (ListServicesUsecaseOutput, error) {
	services, err := u.inventory.ListServices(ctx)
	if err != nil {
		return ListServicesUsecaseOutput{}, err
	}
	out := ListServicesUsecaseOutput{Services: []ServiceSummary{}}
	for _, s := range services {
		instances, err := u.inventory.ListInstances(ctx, s)
		if err != nil {
			return ListServicesUsecaseOutput{}, err
		}
		events, err := u.lifecycle.ListLifecycle(ctx, s, u.retention.RetainedSince(domain.DataInstanceLifecycle, in.Now), in.Now)
		if err != nil {
			return ListServicesUsecaseOutput{}, err
		}
		incs := domain.Incarnations(events)
		sum := ServiceSummary{Name: s}
		for _, i := range instances {
			is := InstanceSummary{Name: i, Incarnations: incs[i]}
			if n := len(is.Incarnations); n > 0 {
				is.Running = !is.Incarnations[n-1].Ended()
			}
			sum.Instances = append(sum.Instances, is)
		}
		out.Services = append(out.Services, sum)
	}
	return out, nil
}
