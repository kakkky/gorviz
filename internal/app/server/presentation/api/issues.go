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

type GetIssuesHandler struct {
	uc  *usecase.ListIssuesUsecase
	gui domain.GUI
}

func NewGetIssuesHandler(uc *usecase.ListIssuesUsecase, gui domain.GUI) *GetIssuesHandler {
	return &GetIssuesHandler{uc: uc, gui: gui}
}

type issueSummaryJSON struct {
	Issue            string     `json:"issue"`
	Kind             string     `json:"kind"`
	Subject          string     `json:"subject"`
	Certainty        string     `json:"certainty"`
	Occurrences      int        `json:"occurrences"`
	LastOccurredAt   *time.Time `json:"lastOccurredAt"`
	Ongoing          bool       `json:"ongoing"`
	OngoingInstances []string   `json:"ongoingInstances"`
	Detail           string     `json:"detail"`
	GUIURL           string     `json:"guiUrl"`
}

type issuesResponse struct {
	Service       string             `json:"service"`
	From          *time.Time         `json:"from"`
	To            *time.Time         `json:"to"`
	RetainedSince *time.Time         `json:"retainedSince"`
	Issues        []issueSummaryJSON `json:"issues"`
	NextCursor    string             `json:"nextCursor,omitempty"`
}

func toIssueSummary(gui domain.GUI, s observation.ServiceName, is usecase.IssueSummary) issueSummaryJSON {
	out := issueSummaryJSON{Issue: is.Name, Kind: string(is.Kind), Subject: is.Subject, Certainty: string(is.Certainty), Occurrences: is.Occurrences,
		LastOccurredAt: utc(is.LastOccurredAt), Ongoing: is.Ongoing, OngoingInstances: []string{},
		Detail: apiPath(serverapi.GetIssue, map[string]string{"service": string(s), "issue": is.Name}, nil), GUIURL: gui.Issue(s, is.Name)}
	for _, i := range is.OngoingInstances {
		out.OngoingInstances = append(out.OngoingInstances, string(i))
	}
	return out
}

func (h *GetIssuesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	q := r.URL.Query()
	from, to, err := parseRange(q, now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	maxBytes, err := parseInt(q, "max-bytes")
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	s := observation.ServiceName(r.PathValue("service"))
	out, err := h.uc.Exec(r.Context(), usecase.ListIssuesUsecaseInput{Service: s, From: from, To: to, Cursor: domain.PageCursor(q.Get("cursor")), MaxBytes: maxBytes, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	res := issuesResponse{Service: string(s), From: utc(out.From), To: utc(out.To), RetainedSince: utc(out.RetainedSince), Issues: []issueSummaryJSON{}, NextCursor: string(out.Next)}
	for _, is := range out.Issues {
		res.Issues = append(res.Issues, toIssueSummary(h.gui, s, is))
	}
	write(rw, r, res)
}

type GetIssueHandler struct {
	uc  *usecase.GetIssueUsecase
	gui domain.GUI
}

func NewGetIssueHandler(uc *usecase.GetIssueUsecase, gui domain.GUI) *GetIssueHandler {
	return &GetIssueHandler{uc: uc, gui: gui}
}

// impactJSON は影響の大きかった goroutine か、スタック・Lock を呼んだ場所ごとの行。Diagnosis はその goroutine の診断を取る API
type impactJSON struct {
	Goroutine         int64   `json:"goroutine,omitempty"`
	Goroutines        []int64 `json:"goroutines,omitempty"`
	Entry             string  `json:"entry,omitempty"`
	Function          string  `json:"function,omitempty"`
	Location          string  `json:"location,omitempty"`
	CreatedBy         string  `json:"createdBy,omitempty"`
	CreatedByLocation string  `json:"createdByLocation,omitempty"`
	Reason            string  `json:"reason,omitempty"`
	Duration          dur     `json:"duration,omitempty"`
	Max               dur     `json:"max,omitempty"`
	Average           dur     `json:"average,omitempty"`
	Count             int     `json:"count,omitempty"`
	Of                int     `json:"of,omitempty"`
	Share             float64 `json:"share,omitempty"`
	Leak              string  `json:"leak,omitempty"`
	HolderGoroutine   int64   `json:"holderGoroutine,omitempty"`
	HolderFunction    string  `json:"holderFunction,omitempty"`
	HolderLocation    string  `json:"holderLocation,omitempty"`
	Certainty         string  `json:"certainty,omitempty"`
	Diagnosis         string  `json:"diagnosis,omitempty"`
	HolderDiagnosis   string  `json:"holderDiagnosis,omitempty"`
	GUIURL            string  `json:"guiUrl,omitempty"`
}

type occurrenceJSON struct {
	Instance       string       `json:"instance"`
	Incarnation    string       `json:"incarnation"`
	Judge          string       `json:"judge"`
	Certainty      string       `json:"certainty"`
	StartedAt      *time.Time   `json:"startedAt"`
	LastSeenAt     *time.Time   `json:"lastSeenAt"`
	EndedAt        *time.Time   `json:"endedAt"`
	Ongoing        bool         `json:"ongoing"`
	Peak           float64      `json:"peak"`
	Reason         string       `json:"reason"`
	TraceFile      string       `json:"traceFile"`
	TraceAvailable bool         `json:"traceAvailable"`
	SchedulerURL   string       `json:"schedulerUrl"`
	Impacts        []impactJSON `json:"impacts"`
	Sites          []impactJSON `json:"sites"`
}

type dayCountJSON struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type issueResponse struct {
	Issue         string           `json:"issue"`
	Kind          string           `json:"kind"`
	Subject       string           `json:"subject"`
	Certainty     string           `json:"certainty"`
	CertaintyNote string           `json:"certaintyNote"`
	NextAction    string           `json:"nextAction"`
	Ongoing       bool             `json:"ongoing"`
	GUIURL        string           `json:"guiUrl"`
	RetainedSince *time.Time       `json:"retainedSince"`
	CountsByDay   []dayCountJSON   `json:"countsByDay"`
	Occurrences   []occurrenceJSON `json:"occurrences"`
}

func (h *GetIssueHandler) toImpacts(s observation.ServiceName, issue string, o domain.Occurrence, in []domain.Impact) []impactJSON {
	from, to := o.Around()
	q := url.Values{"issue": {issue}, "occurrence": {rfc(o.StartedAt)}}
	out := []impactJSON{}
	for _, m := range in {
		j := impactJSON{Goroutine: int64(m.Goroutine), Entry: m.Entry, Function: m.Function, Location: m.Location, CreatedBy: m.CreatedBy, CreatedByLocation: m.CreatedByLocation,
			Reason: m.Reason, Duration: dur(m.Duration), Max: dur(m.Max), Count: m.Count, Of: m.Of, Share: m.Share, HolderGoroutine: int64(m.HolderGoroutine),
			HolderFunction: m.HolderFunction, HolderLocation: m.HolderLocation, Certainty: string(m.Certainty)}
		if m.Count > 0 && m.Duration > 0 {
			j.Average = dur(m.Duration / time.Duration(m.Count))
		}
		for _, g := range m.Goroutines {
			j.Goroutines = append(j.Goroutines, int64(g))
		}
		if m.Of > 0 && len(m.Goroutines) > 0 {
			j.Leak = "同じ場所で待っている " + itoa(m.Of) + " 個のうち " + itoa(m.Count) + " 個がリーク"
		}
		first := m.Goroutine
		if first == 0 && len(m.Goroutines) > 0 {
			first = m.Goroutines[0]
		}
		if first != 0 {
			j.Diagnosis = diagnosisPath(s, o.Instance, first, q)
			j.GUIURL = h.gui.Goroutine(s, o.Instance, first, from, to)
		}
		if m.HolderGoroutine != 0 {
			j.HolderDiagnosis = diagnosisPath(s, o.Instance, m.HolderGoroutine, q)
		}
		out = append(out, j)
	}
	return out
}

func (h *GetIssueHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	from, to, err := parseRange(r.URL.Query(), now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	s, name := observation.ServiceName(r.PathValue("service")), r.PathValue("issue")
	out, err := h.uc.Exec(r.Context(), usecase.GetIssueUsecaseInput{Service: s, Issue: name, From: from, To: to, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	sum := out.Summary
	res := issueResponse{Issue: sum.Name, Kind: string(sum.Kind), Subject: sum.Subject, Certainty: string(sum.Certainty), CertaintyNote: certaintyNote(sum.Certainty),
		NextAction: sum.NextAction, Ongoing: sum.Ongoing, GUIURL: h.gui.Issue(s, sum.Name), RetainedSince: utc(out.RetainedSince), CountsByDay: []dayCountJSON{}, Occurrences: []occurrenceJSON{}}
	for _, d := range out.CountsByDay {
		res.CountsByDay = append(res.CountsByDay, dayCountJSON{Day: d.Day.Format("2006-01-02"), Count: d.Count})
	}
	for _, o := range out.Occurrences {
		f, t := o.Around()
		res.Occurrences = append(res.Occurrences, occurrenceJSON{
			Instance: string(o.Instance), Incarnation: string(o.Incarnation), Judge: string(o.Judge), Certainty: string(o.Certainty),
			StartedAt: utc(o.StartedAt), LastSeenAt: utc(o.LastSeenAt), EndedAt: utc(o.EndedAt), Ongoing: o.Ongoing(), Peak: o.Peak, Reason: o.Reason,
			TraceFile: o.TraceFile, TraceAvailable: o.TraceRetained, SchedulerURL: h.gui.Scheduler(s, o.Instance, f, t),
			Impacts: h.toImpacts(s, sum.Name, o.Occurrence, o.Impacts), Sites: h.toImpacts(s, sum.Name, o.Occurrence, o.Sites),
		})
	}
	write(rw, r, res)
}
