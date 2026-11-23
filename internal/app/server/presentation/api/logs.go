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

type logSummaryJSON struct {
	ID       string     `json:"id"`
	At       *time.Time `json:"at"`
	Instance string     `json:"instance"`
	Level    string     `json:"level"`
	Message  string     `json:"message"`
	Duration *dur       `json:"duration"`
}

func toLogSummary(l domain.StoredLog) logSummaryJSON {
	out := logSummaryJSON{ID: string(l.ID()), At: utc(l.At), Instance: string(l.Instance), Level: string(l.Level), Message: l.Message}
	if l.HasDuration {
		out.Duration = ptr(dur(l.Duration))
	}
	return out
}

type GetLogsHandler struct {
	uc *usecase.SearchLogsUsecase
}

func NewGetLogsHandler(uc *usecase.SearchLogsUsecase) *GetLogsHandler {
	return &GetLogsHandler{uc: uc}
}

type logsResponse struct {
	Service       string           `json:"service"`
	From          *time.Time       `json:"from"`
	To            *time.Time       `json:"to"`
	RetainedSince *time.Time       `json:"retainedSince"`
	Logs          []logSummaryJSON `json:"logs"`
	NextCursor    string           `json:"nextCursor,omitempty"`
	Detail        string           `json:"detail"`
}

func (h *GetLogsHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	q := r.URL.Query()
	from, to, err := parseRange(q, now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	filter, err := parseFilter(q)
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
	out, err := h.uc.Exec(r.Context(), usecase.SearchLogsUsecaseInput{Service: s, From: from, To: to, Filter: filter, After: domain.LogID(q.Get("cursor")), MaxBytes: maxBytes, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	res := logsResponse{Service: string(s), From: utc(out.From), To: utc(out.To), RetainedSince: utc(out.RetainedSince), Logs: []logSummaryJSON{}, NextCursor: string(out.Next),
		Detail: apiPath(serverapi.GetLog, map[string]string{"service": string(s), "log": "{id}"}, nil)}
	for _, l := range out.Logs {
		res.Logs = append(res.Logs, toLogSummary(l))
	}
	write(rw, r, res)
}

type GetLogCountsHandler struct {
	uc *usecase.CountLogsUsecase
}

func NewGetLogCountsHandler(uc *usecase.CountLogsUsecase) *GetLogCountsHandler {
	return &GetLogCountsHandler{uc: uc}
}

type logCountJSON struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type logCountsResponse struct {
	Service       string         `json:"service"`
	From          *time.Time     `json:"from"`
	To            *time.Time     `json:"to"`
	By            string         `json:"by"`
	Total         int            `json:"total"`
	Missing       int            `json:"missing"`
	Others        int            `json:"others"`
	RetainedSince *time.Time     `json:"retainedSince"`
	Counts        []logCountJSON `json:"counts"`
}

func (h *GetLogCountsHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	q := r.URL.Query()
	from, to, err := parseRange(q, now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	filter, err := parseFilter(q)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	s := observation.ServiceName(r.PathValue("service"))
	out, err := h.uc.Exec(r.Context(), usecase.CountLogsUsecaseInput{Service: s, From: from, To: to, Filter: filter, By: q.Get("by"), Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	res := logCountsResponse{Service: string(s), From: utc(out.From), To: utc(out.To), By: out.By, Total: out.Total, Missing: out.Missing, Others: out.Others,
		RetainedSince: utc(out.RetainedSince), Counts: []logCountJSON{}}
	for _, c := range out.Counts {
		res.Counts = append(res.Counts, logCountJSON{Value: c.Value, Count: c.Count})
	}
	write(rw, r, res)
}

type GetLogHandler struct {
	uc  *usecase.GetLogUsecase
	gui domain.GUI
}

func NewGetLogHandler(uc *usecase.GetLogUsecase, gui domain.GUI) *GetLogHandler {
	return &GetLogHandler{uc: uc, gui: gui}
}

type writerJSON struct {
	Status        string `json:"status"`
	Goroutine     int64  `json:"goroutine,omitempty"`
	Certainty     string `json:"certainty,omitempty"`
	CertaintyNote string `json:"certaintyNote,omitempty"`
	Diagnosis     string `json:"diagnosis,omitempty"`
	StateChanges  string `json:"stateChanges,omitempty"`
	GUIURL        string `json:"guiUrl,omitempty"`
}

type logResponse struct {
	ID              string            `json:"id"`
	At              *time.Time        `json:"at"`
	TimeFromLog     bool              `json:"timeFromLog"`
	Instance        string            `json:"instance"`
	Incarnation     string            `json:"incarnation"`
	Level           string            `json:"level"`
	Message         string            `json:"message"`
	Duration        *dur              `json:"duration"`
	Source          string            `json:"source"`
	Attrs           map[string]string `json:"attrs"`
	Raw             string            `json:"raw"`
	Period          periodJSON        `json:"period"`
	Writer          writerJSON        `json:"writer"`
	SchedulerURL    string            `json:"schedulerUrl"`
	GUIURL          string            `json:"guiUrl"`
	PreservedTraces []preservedJSON   `json:"preservedTraces"`
}

func (h *GetLogHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	s := observation.ServiceName(r.PathValue("service"))
	out, err := h.uc.Exec(r.Context(), usecase.GetLogUsecaseInput{Service: s, Log: domain.LogID(r.PathValue("log")), Now: time.Now()})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	l := out.Log
	res := logResponse{ID: string(l.ID()), At: utc(l.At), TimeFromLog: l.TimeFromLog, Instance: string(l.Instance), Incarnation: string(l.Incarnation),
		Level: string(l.Level), Message: l.Message, Source: string(l.Source), Attrs: out.Attrs, Raw: l.Raw, Period: toPeriod(out.Period),
		Writer:          writerJSON{Status: string(out.WriterStatus)},
		SchedulerURL:    h.gui.Scheduler(s, l.Instance, out.Period.From, out.Period.To),
		GUIURL:          h.gui.Log(s, l.ID()),
		PreservedTraces: toPreserved(h.gui, s, l.Instance, out.Preserved),
	}
	if l.HasDuration {
		res.Duration = ptr(dur(l.Duration))
	}
	if out.WriterStatus == usecase.WriterFound {
		g := out.Writer.Goroutine
		q := url.Values{"log": {string(l.ID())}}
		res.Writer.Goroutine, res.Writer.Certainty, res.Writer.CertaintyNote = int64(g), string(out.Writer.Certainty), certaintyNote(out.Writer.Certainty)
		res.Writer.Diagnosis = diagnosisPath(s, l.Instance, g, q)
		res.Writer.StateChanges = apiPath(serverapi.ListStateChanges, map[string]string{"service": string(s), "instance": string(l.Instance), "goroutine": itoa(int(g))}, q)
		res.Writer.GUIURL = h.gui.Goroutine(s, l.Instance, g, out.Period.From, out.Period.To)
	}
	write(rw, r, res)
}
