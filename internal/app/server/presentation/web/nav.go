package web

import (
	"context"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/layout"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const (
	tabOverview = "Overview"
	tabIssues   = "Issues"
	tabLogs     = "Logs"
)

// navigator は見出しのサービスの切り替えと、インスタンスの一覧を組む。どの画面の handler も使う
type navigator struct {
	services *usecase.ListServicesUsecase
}

// base は見出し。tab はサービス単位の画面の名前、instance はインスタンス単位の画面で見ているインスタンス
func (n navigator) base(ctx context.Context, title string, s observation.ServiceName, i observation.InstanceName, tab string) (layout.BaseViewModel, usecase.ServiceSummary, error) {
	out, err := n.services.Exec(ctx, usecase.ListServicesUsecaseInput{Now: time.Now()})
	if err != nil {
		return layout.BaseViewModel{}, usecase.ServiceSummary{}, err
	}
	vm := layout.BaseViewModel{Title: title, Service: string(s), Instance: string(i)}
	var current usecase.ServiceSummary
	found := false
	for _, svc := range out.Services {
		vm.Services = append(vm.Services, layout.LinkViewModel{Label: string(svc.Name), URL: paths.Overview(svc.Name), Active: svc.Name == s})
		if svc.Name == s {
			current, found = svc, true
		}
	}
	if !found {
		names := make([]string, 0, len(out.Services))
		for _, svc := range out.Services {
			names = append(names, string(svc.Name))
		}
		return layout.BaseViewModel{}, usecase.ServiceSummary{}, domain.ErrNotFound.WithCandidates("no service named "+string(s), names)
	}
	vm.Tabs = []layout.LinkViewModel{
		{Label: tabOverview, URL: paths.Overview(s), Active: tab == tabOverview},
		{Label: tabIssues, URL: servicePath(s, "issues"), Active: tab == tabIssues},
		{Label: tabLogs, URL: servicePath(s, "logs"), Active: tab == tabLogs},
	}
	instances := slices.Clone(current.Instances)
	slices.SortStableFunc(instances, func(a, b usecase.InstanceSummary) int { return boolFirst(a.Running, b.Running) })
	for _, inst := range instances {
		vm.Instances = append(vm.Instances, layout.InstanceLinkViewModel{Label: string(inst.Name), URL: paths.Scheduler(s, inst.Name, time.Time{}, time.Time{}), Active: inst.Name == i, Running: inst.Running})
	}
	return vm, current, nil
}

func boolFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}
