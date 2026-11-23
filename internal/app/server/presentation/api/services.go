package api

import (
	"net/http"
	"net/url"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

type GetServicesHandler struct {
	uc  *usecase.ListServicesUsecase
	gui domain.GUI
}

func NewGetServicesHandler(uc *usecase.ListServicesUsecase, gui domain.GUI) *GetServicesHandler {
	return &GetServicesHandler{uc: uc, gui: gui}
}

type incarnationJSON struct {
	Incarnation string     `json:"incarnation"`
	StartedAt   *time.Time `json:"startedAt"`
	EndedAt     *time.Time `json:"endedAt"`
	Restart     bool       `json:"restart"`
}

type instanceJSON struct {
	Name          string            `json:"name"`
	Running       bool              `json:"running"`
	Incarnations  []incarnationJSON `json:"incarnations"`
	AvailableData string            `json:"availableData"`
}

type serviceJSON struct {
	Name        string         `json:"name"`
	OverviewURL string         `json:"overviewUrl"`
	Overview    string         `json:"overview"`
	Issues      string         `json:"issues"`
	Instances   []instanceJSON `json:"instances"`
}

type servicesResponse struct {
	Services []serviceJSON `json:"services"`
}

func (h *GetServicesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	out, err := h.uc.Exec(r.Context(), usecase.ListServicesUsecaseInput{Now: time.Now()})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	res := servicesResponse{Services: []serviceJSON{}}
	for _, s := range out.Services {
		p := map[string]string{"service": string(s.Name)}
		sj := serviceJSON{Name: string(s.Name), OverviewURL: h.gui.Overview(s.Name), Overview: apiPath(serverapi.GetOverview, p, nil), Issues: apiPath(serverapi.ListIssues, p, nil), Instances: []instanceJSON{}}
		for _, i := range s.Instances {
			ij := instanceJSON{Name: string(i.Name), Running: i.Running, Incarnations: []incarnationJSON{},
				AvailableData: apiPath(serverapi.ListAvailableData, map[string]string{"service": string(s.Name), "instance": string(i.Name)}, nil)}
			for _, inc := range i.Incarnations {
				ij.Incarnations = append(ij.Incarnations, incarnationJSON{Incarnation: string(inc.Incarnation), StartedAt: utc(inc.StartedAt), EndedAt: utc(inc.EndedAt), Restart: inc.Restart})
			}
			sj.Instances = append(sj.Instances, ij)
		}
		res.Services = append(res.Services, sj)
	}
	write(rw, r, res)
}

type GetOverviewHandler struct {
	uc  *usecase.GetOverviewUsecase
	gui domain.GUI
}

func NewGetOverviewHandler(uc *usecase.GetOverviewUsecase, gui domain.GUI) *GetOverviewHandler {
	return &GetOverviewHandler{uc: uc, gui: gui}
}

// pointJSON は推移の 1 点。値のない欄 (その区切りにデータがない) は null
type pointJSON struct {
	At                 *time.Time `json:"at"`
	Goroutines         *int       `json:"goroutines"`
	Logs               int        `json:"logs"`
	Errors             int        `json:"errors"`
	RunnableP99        *dur       `json:"runnableToRunningP99"`
	RunnableShare      *float64   `json:"runnableShare"`
	GCCPUShare         *float64   `json:"gcCpuShare"`
	MarkAssistCPUShare *float64   `json:"markAssistCpuShare"`
	LockWaitPerRunning *float64   `json:"lockWaitPerRunning"`
	HeapBytes          *uint64    `json:"heapBytes"`
	HeapGoalBytes      *uint64    `json:"heapGoalBytes"`
	Threads            *int       `json:"threads"`
}

type ongoingJSON struct {
	Issue          string     `json:"issue"`
	Kind           string     `json:"kind"`
	Subject        string     `json:"subject"`
	Certainty      string     `json:"certainty"`
	Instances      []string   `json:"instances"`
	LastOccurredAt *time.Time `json:"lastOccurredAt"`
	Detail         string     `json:"detail"`
}

type lifecycleJSON struct {
	At          *time.Time `json:"at"`
	Instance    string     `json:"instance"`
	Incarnation string     `json:"incarnation"`
	Kind        string     `json:"kind"`
	Restart     bool       `json:"restart"`
}

type overviewResponse struct {
	Service       string          `json:"service"`
	From          *time.Time      `json:"from"`
	To            *time.Time      `json:"to"`
	Step          dur             `json:"step"`
	GUIURL        string          `json:"guiUrl"`
	RetainedSince *time.Time      `json:"retainedSince"`
	OngoingIssues []ongoingJSON   `json:"ongoingIssues"`
	Lifecycle     []lifecycleJSON `json:"lifecycle"`
	Points        []pointJSON     `json:"points"`
}

func ptr[T any](v T) *T { return &v }

func toPoint(m domain.MetricSample) pointJSON {
	p := pointJSON{At: utc(m.At), Logs: m.Logs, Errors: m.Errors}
	if m.HasGoroutines {
		p.Goroutines = ptr(m.Goroutines)
	}
	if m.Traced > 0 {
		if d, ok := m.SchedP99(); ok {
			p.RunnableP99 = ptr(dur(d))
		}
		p.RunnableShare, p.GCCPUShare, p.MarkAssistCPUShare = ptr(m.RunnableShare()), ptr(m.GCShare()), ptr(m.MarkAssistShare())
		if m.Running > 0 {
			p.LockWaitPerRunning = ptr(m.LockWaitRatio())
		}
		p.Threads = ptr(m.Threads)
	}
	if m.HeapBytes > 0 {
		p.HeapBytes, p.HeapGoalBytes = ptr(m.HeapBytes), ptr(m.HeapGoalBytes)
	}
	return p
}

func (h *GetOverviewHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	from, to, err := parseRange(r.URL.Query(), now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	s := observation.ServiceName(r.PathValue("service"))
	out, err := h.uc.Exec(r.Context(), usecase.GetOverviewUsecaseInput{Service: s, From: from, To: to, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	res := overviewResponse{Service: string(s), From: utc(out.From), To: utc(out.To), Step: dur(out.Step), GUIURL: h.gui.Overview(s), RetainedSince: utc(out.RetainedSince),
		OngoingIssues: []ongoingJSON{}, Lifecycle: []lifecycleJSON{}, Points: []pointJSON{}}
	for _, is := range out.OngoingIssues {
		o := ongoingJSON{Issue: is.Name, Kind: string(is.Kind), Subject: is.Subject, Certainty: string(is.Certainty), LastOccurredAt: utc(is.LastOccurredAt),
			Detail: apiPath(serverapi.GetIssue, map[string]string{"service": string(s), "issue": is.Name}, url.Values{})}
		for _, i := range is.OngoingInstances {
			o.Instances = append(o.Instances, string(i))
		}
		res.OngoingIssues = append(res.OngoingIssues, o)
	}
	for _, e := range out.Lifecycle {
		res.Lifecycle = append(res.Lifecycle, lifecycleJSON{At: utc(e.At), Instance: string(e.Instance), Incarnation: string(e.Incarnation), Kind: string(e.Kind), Restart: e.Restart})
	}
	for _, p := range out.Points {
		res.Points = append(res.Points, toPoint(p))
	}
	write(rw, r, res)
}
