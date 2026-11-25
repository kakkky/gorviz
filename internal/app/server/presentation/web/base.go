package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/layout"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const (
	tabOverview  = "Overview"
	tabIssues    = "Issues"
	tabScheduler = "Scheduler"
)

func at(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64) + "s"
}

func span(from, to time.Duration) string {
	return at(from) + " – " + at(to)
}

func issueURL(id domain.IssueID, n int) string {
	if n > 0 {
		return fmt.Sprintf("/issues/%s?occurrence=%d", id, n)
	}
	return "/issues/" + string(id)
}

func goroutineURL(id domain.GoroutineID, from, to time.Duration) string {
	return fmt.Sprintf("/goroutines/%d?from=%s&to=%s", id, at(from), at(to))
}

func schedulerURL(from, to time.Duration) string {
	return fmt.Sprintf("/scheduler?from=%s&to=%s", at(from), at(to))
}

func pct(v, total time.Duration) string {
	if total <= 0 {
		return "0%"
	}
	return strconv.FormatFloat(float64(v)*100/float64(total), 'f', 3, 64) + "%"
}

// focus は画面が見ている範囲。時間軸の上に重ねる
type focus struct {
	from, to time.Duration
	has      bool
}

func baseOf(title, tab string, st usecase.GetSessionStatusUsecaseOutput, f focus) layout.BaseViewModel {
	app := st.Session.App
	vm := layout.BaseViewModel{Title: title, App: app.Package, State: string(app.State), Running: app.State == domain.AppRunning && st.AgentConnected, Elapsed: at(st.Elapsed)}
	for _, t := range []struct{ label, url string }{{tabOverview, "/overview"}, {tabIssues, "/issues"}, {tabScheduler, "/scheduler"}} {
		vm.Tabs = append(vm.Tabs, layout.LinkViewModel{Label: t.label, URL: t.url, Active: t.label == tab})
	}
	vm.Timeline = timelineOf(st.Timeline, st.Elapsed, f)
	return vm
}

func timelineOf(events []domain.SessionEvent, elapsed time.Duration, f focus) components.TimelineViewModel {
	total := max(elapsed, time.Millisecond)
	vm := components.TimelineViewModel{Elapsed: at(elapsed)}
	for i, e := range events {
		end := elapsed
		if i+1 < len(events) {
			end = events[i+1].At
		}
		switch e.Kind {
		case domain.EventPhase, domain.EventShutdown:
			name, kind := e.Phase, "load"
			if e.Kind == domain.EventShutdown {
				name, kind = domain.PhaseShutdown, "shutdown"
			}
			vm.Phases = append(vm.Phases, components.TimelinePhaseViewModel{Name: name, X: pct(e.At, total), W: pct(end-e.At, total), URL: schedulerURL(e.At, min(end, e.At+2*time.Second)), Title: name + " " + span(e.At, end), Kind: kind})
		default:
			label := string(e.Kind)
			title := label + " at " + at(e.At)
			if e.Note != "" {
				title += " (" + e.Note + ")"
			}
			vm.Events = append(vm.Events, components.TimelineEventViewModel{X: pct(e.At, total), Label: label, Title: title})
		}
	}
	if f.has {
		vm.HasFocus = true
		vm.FocusX, vm.FocusW = pct(f.from, total), pct(max(f.to-f.from, total/500), total)
	}
	return vm
}

func rangeOf(r *http.Request) (time.Duration, time.Duration, bool, error) {
	q := r.URL.Query()
	if q.Get("from") == "" && q.Get("to") == "" {
		return 0, 0, false, nil
	}
	from, err := time.ParseDuration(q.Get("from"))
	if err != nil {
		return 0, 0, false, domain.ErrInvalidArgument.With("from must be a time since the start of the session, like 12.5s")
	}
	to, err := time.ParseDuration(q.Get("to"))
	if err != nil {
		return 0, 0, false, domain.ErrInvalidArgument.With("to must be a time since the start of the session, like 14s")
	}
	return from, to, true, nil
}
