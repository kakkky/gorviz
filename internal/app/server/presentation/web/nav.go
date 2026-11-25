package web

import (
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/layout"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const (
	tabOverview  = "Overview"
	tabIssues    = "Issues"
	tabScheduler = "Scheduler"
)

func baseOf(title, tab string, st usecase.GetSessionStatusUsecaseOutput) layout.BaseViewModel {
	app := st.Session.App
	state := string(app.State) + " · " + at(st.Elapsed)
	if app.Exited {
		state = "exited " + itoa(app.ExitCode) + " · " + at(st.Elapsed)
	}
	vm := layout.BaseViewModel{Title: title, App: app.Package, State: state, Running: app.State == domain.AppRunning && st.AgentConnected}
	for _, t := range []struct{ label, url string }{{tabOverview, "/overview"}, {tabIssues, "/issues"}, {tabScheduler, "/scheduler"}} {
		vm.Tabs = append(vm.Tabs, layout.LinkViewModel{Label: t.label, URL: t.url, Active: t.label == tab})
	}
	return vm
}
