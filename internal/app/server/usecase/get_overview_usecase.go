package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// overviewMaxPoints は推移の点の数の上限。期間が長ければ区切りを粗くする
const overviewMaxPoints = 360

type GetOverviewUsecaseInput struct {
	Service observation.ServiceName
	From    time.Time
	To      time.Time
	Now     time.Time
}

// GetOverviewUsecaseOutput の Points はサービスの値を Step ごとに束ねたもの。値は MetricSample の SchedP99・GCShare などで読む
type GetOverviewUsecaseOutput struct {
	Service       observation.ServiceName
	From          time.Time
	To            time.Time
	Step          time.Duration
	Points        []domain.MetricSample
	OngoingIssues []IssueSummary
	Lifecycle     []domain.LifecycleEvent
	RetainedSince time.Time
}

type GetOverviewUsecase struct {
	retention domain.Retention
	inventory domain.InventoryAccessor
	metrics   domain.MetricAccessor
	issues    domain.IssueAccessor
	lifecycle domain.LifecycleAccessor
}

func (us *UseCase) NewGetOverviewUsecase() *GetOverviewUsecase {
	return &GetOverviewUsecase{retention: us.settings.Retention, inventory: us.inventory, metrics: us.metrics, issues: us.issues, lifecycle: us.lifecycle}
}

func (u *GetOverviewUsecase) Exec(ctx context.Context, in GetOverviewUsecaseInput) (GetOverviewUsecaseOutput, error) {
	if err := requireService(ctx, u.inventory, in.Service); err != nil {
		return GetOverviewUsecaseOutput{}, err
	}
	from, to := in.From, in.To
	if to.IsZero() {
		to = in.Now
	}
	if from.IsZero() {
		from = to.Add(-time.Hour)
	}
	if !from.Before(to) {
		return GetOverviewUsecaseOutput{}, domain.ErrInvalidArgument.With("from must be before to")
	}
	step := domain.MetricStepFor(to.Sub(from), overviewMaxPoints)
	samples, err := u.metrics.ListMetrics(ctx, in.Service, from.Truncate(step), to)
	if err != nil {
		return GetOverviewUsecaseOutput{}, err
	}
	days, err := u.issues.ListIssues(ctx, in.Service, u.retention.RetainedSince(domain.DataIssues, in.Now), in.Now)
	if err != nil {
		return GetOverviewUsecaseOutput{}, err
	}
	var ongoing []IssueSummary
	for _, is := range summarizeIssues(days, time.Time{}, in.Now) {
		if is.Ongoing {
			ongoing = append(ongoing, is)
		}
	}
	events, err := u.lifecycle.ListLifecycle(ctx, in.Service, from, to)
	if err != nil {
		return GetOverviewUsecaseOutput{}, err
	}
	return GetOverviewUsecaseOutput{
		Service: in.Service, From: from, To: to, Step: step, Points: domain.Rollup(samples, from, to, step), OngoingIssues: ongoing,
		Lifecycle: events, RetainedSince: u.retention.RetainedSince(domain.DataMetrics, in.Now),
	}, nil
}
