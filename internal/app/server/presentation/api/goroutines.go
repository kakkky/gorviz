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

type causeJSON struct {
	Cause     string `json:"cause"`
	Certainty string `json:"certainty"`
	Note      string `json:"note"`
}

type breakdownJSON struct {
	Cause    string  `json:"cause"`
	Duration dur     `json:"duration"`
	Share    float64 `json:"share"`
}

type stopJSON struct {
	Cause      string `json:"cause"`
	Function   string `json:"function"`
	Location   string `json:"location"`
	WaitReason string `json:"waitReason,omitempty"`
	Duration   dur    `json:"duration"`
}

type frameJSON struct {
	Function string `json:"function"`
	Location string `json:"location"`
}

type leakJSON struct {
	Checked   bool       `json:"checked"`
	Status    string     `json:"status"`
	CheckedAt *time.Time `json:"checkedAt"`
	Leaked    bool       `json:"leaked"`
	Certainty string     `json:"certainty,omitempty"`
	Count     int        `json:"count,omitempty"`
	Of        int        `json:"of,omitempty"`
	Note      string     `json:"note,omitempty"`
}

type wakerJSON struct {
	Goroutine int64  `json:"goroutine"`
	Function  string `json:"function"`
	Location  string `json:"location"`
	Cause     string `json:"cause"`
	Count     int    `json:"count"`
	Waited    dur    `json:"waited"`
	Diagnosis string `json:"diagnosis"`
}

type computedFromJSON struct {
	Source             string     `json:"source"`
	From               *time.Time `json:"from"`
	To                 *time.Time `json:"to"`
	MinuteAligned      bool       `json:"minuteAligned"`
	FromPreservedTrace bool       `json:"fromPreservedTrace"`
}

type diagnosisResponse struct {
	Service      string            `json:"service"`
	Instance     string            `json:"instance"`
	Goroutine    int64             `json:"goroutine"`
	Period       periodJSON        `json:"period"`
	Found        bool              `json:"found"`
	ComputedFrom computedFromJSON  `json:"computedFrom"`
	Entry        string            `json:"entry"`
	CreatedBy    *frameJSON        `json:"createdBy"`
	MainCause    *causeJSON        `json:"mainCause"`
	Breakdown    []breakdownJSON   `json:"breakdown"`
	Unrecorded   *dur              `json:"unrecorded"`
	StoppedAt    *stopJSON         `json:"stoppedAt"`
	Leak         leakJSON          `json:"leak"`
	WokenBy      []wakerJSON       `json:"wokenBy"`
	Logs         []logSummaryJSON  `json:"logs"`
	Unavailable  []unavailableJSON `json:"unavailable"`
	NearbyTraces []preservedJSON   `json:"nearbyTraces"`
	StateChanges string            `json:"stateChanges"`
	GUIURL       string            `json:"guiUrl"`
	SchedulerURL string            `json:"schedulerUrl"`
}

type GetGoroutineDiagnosisHandler struct {
	uc  *usecase.DiagnoseGoroutineUsecase
	gui domain.GUI
}

func NewGetGoroutineDiagnosisHandler(uc *usecase.DiagnoseGoroutineUsecase, gui domain.GUI) *GetGoroutineDiagnosisHandler {
	return &GetGoroutineDiagnosisHandler{uc: uc, gui: gui}
}

func toBreakdown(b *domain.StateBreakdown) []breakdownJSON {
	out := []breakdownJSON{}
	if b == nil {
		return out
	}
	total := b.Total()
	for _, c := range domain.MainCauses {
		if d := b.Durations[c]; d > 0 {
			out = append(out, breakdownJSON{Cause: string(c), Duration: dur(d), Share: float64(d) / float64(total)})
		}
	}
	return out
}

func leakNote(l domain.LeakStatus) string {
	switch {
	case !l.Checked && l.Status == "":
		return "no leak check is kept"
	case !l.Checked:
		return "the runtime cannot check leaks (" + string(l.Status) + ")"
	case l.Leaked:
		return itoa(l.Count) + " of the " + itoa(l.Of) + " goroutines with the same stack leaked"
	}
	return "not judged as leaked"
}

func toLeak(l domain.LeakStatus) leakJSON {
	return leakJSON{Checked: l.Checked, Status: string(l.Status), CheckedAt: utc(l.CheckedAt), Leaked: l.Leaked, Certainty: string(l.Certainty), Count: l.Count, Of: l.Of, Note: leakNote(l)}
}

func goroutineParams(r *http.Request) (instanceParams, observation.GoroutineID, error) {
	p := pathInstance(r)
	g, err := parseGoroutine(r.PathValue("goroutine"), "goroutine")
	if err == nil && g == 0 {
		err = domain.ErrInvalidArgument.With("goroutine must be a goroutine id")
	}
	return p, g, err
}

func (h *GetGoroutineDiagnosisHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	p, g, err := goroutineParams(r)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	pq, err := parsePeriodQuery(r.URL.Query(), now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	out, err := h.uc.Exec(r.Context(), usecase.DiagnoseGoroutineUsecaseInput{Service: p.service, Instance: p.instance, Goroutine: g, Period: pq, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	d := out.Diagnosis
	q := rangeQuery(out.Period.From, out.Period.To)
	res := diagnosisResponse{
		Service: string(p.service), Instance: string(p.instance), Goroutine: int64(g), Period: toPeriod(out.Period), Found: out.Found,
		ComputedFrom: computedFromJSON{Source: string(d.Source), From: utc(d.ComputedFrom.From), To: utc(d.ComputedFrom.To), MinuteAligned: d.MinuteAligned, FromPreservedTrace: out.TraceFromPreserved},
		Entry:        d.Entry, Breakdown: toBreakdown(d.Breakdown), Leak: toLeak(out.Leak), WokenBy: []wakerJSON{}, Logs: []logSummaryJSON{},
		Unavailable: toUnavailable(out.Unavailable), NearbyTraces: toPreserved(h.gui, p.service, p.instance, out.NearbyTraces),
		StateChanges: apiPath(serverapi.ListStateChanges, map[string]string{"service": string(p.service), "instance": string(p.instance), "goroutine": itoa(int(g))}, q),
		GUIURL:       h.gui.Goroutine(p.service, p.instance, g, out.Period.From, out.Period.To),
		SchedulerURL: h.gui.Scheduler(p.service, p.instance, out.Period.From, out.Period.To),
	}
	if d.CreatedBy.Function != "" {
		res.CreatedBy = &frameJSON{Function: d.CreatedBy.Function, Location: d.CreatedBy.Location()}
	}
	if out.Found {
		res.MainCause = &causeJSON{Cause: string(d.Main), Certainty: string(d.MainCertainty), Note: certaintyNote(d.MainCertainty)}
	}
	if d.Breakdown != nil {
		res.Unrecorded = ptr(dur(d.Breakdown.Unrecorded))
	}
	if d.Stop != nil {
		res.StoppedAt = &stopJSON{Cause: string(d.Stop.Cause), Function: d.Stop.Function, Location: d.Stop.Location, WaitReason: d.Stop.WaitReason, Duration: dur(d.Stop.Duration)}
	}
	for _, w := range d.Wakers {
		wq := rangeQuery(w.From, w.To)
		wq.Set("woken", itoa(int(g)))
		res.WokenBy = append(res.WokenBy, wakerJSON{Goroutine: int64(w.Goroutine), Function: w.Function, Location: w.Location, Cause: string(w.Cause), Count: w.Count, Waited: dur(w.Waited),
			Diagnosis: diagnosisPath(p.service, p.instance, w.Goroutine, wq)})
	}
	for _, l := range out.Logs {
		res.Logs = append(res.Logs, toLogSummary(l))
	}
	write(rw, r, res)
}

type stateChangeJSON struct {
	At              *time.Time `json:"at"`
	Place           string     `json:"place"`
	Cause           string     `json:"cause"`
	Reason          string     `json:"reason"`
	Function        string     `json:"function"`
	Location        string     `json:"location"`
	Proc            *int64     `json:"proc"`
	Thread          int64      `json:"thread"`
	Created         bool       `json:"created"`
	WokenBy         int64      `json:"wokenBy"`
	WokenByFunction string     `json:"wokenByFunction"`
	WokenByLocation string     `json:"wokenByLocation"`
}

type stateChangesResponse struct {
	Service            string            `json:"service"`
	Instance           string            `json:"instance"`
	Goroutine          int64             `json:"goroutine"`
	Period             periodJSON        `json:"period"`
	Found              bool              `json:"found"`
	FromPreservedTrace bool              `json:"fromPreservedTrace"`
	Changes            []stateChangeJSON `json:"changes"`
	NextCursor         string            `json:"nextCursor,omitempty"`
	Unavailable        []unavailableJSON `json:"unavailable"`
	NearbyTraces       []preservedJSON   `json:"nearbyTraces"`
	Diagnosis          string            `json:"diagnosis"`
	SchedulerURL       string            `json:"schedulerUrl"`
}

type GetGoroutineStateChangesHandler struct {
	uc  *usecase.ListGoroutineStateChangesUsecase
	gui domain.GUI
}

func NewGetGoroutineStateChangesHandler(uc *usecase.ListGoroutineStateChangesUsecase, gui domain.GUI) *GetGoroutineStateChangesHandler {
	return &GetGoroutineStateChangesHandler{uc: uc, gui: gui}
}

func (h *GetGoroutineStateChangesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	p, g, err := goroutineParams(r)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	q := r.URL.Query()
	pq, err := parsePeriodQuery(q, now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	maxBytes, err := parseInt(q, "max-bytes")
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	out, err := h.uc.Exec(r.Context(), usecase.ListGoroutineStateChangesUsecaseInput{Service: p.service, Instance: p.instance, Goroutine: g, Period: pq,
		Cursor: domain.PageCursor(q.Get("cursor")), MaxBytes: maxBytes, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	res := stateChangesResponse{Service: string(p.service), Instance: string(p.instance), Goroutine: int64(g), Period: toPeriod(out.Period), Found: out.Found,
		FromPreservedTrace: out.TraceFromPreserved, Changes: []stateChangeJSON{}, NextCursor: string(out.Next), Unavailable: toUnavailable(out.Unavailable),
		NearbyTraces: toPreserved(h.gui, p.service, p.instance, out.NearbyTraces),
		Diagnosis:    diagnosisPath(p.service, p.instance, g, rangeQuery(out.Period.From, out.Period.To)),
		SchedulerURL: h.gui.Scheduler(p.service, p.instance, out.Period.From, out.Period.To)}
	for _, c := range out.Changes {
		sc := stateChangeJSON{At: utc(c.At), Place: string(c.Place), Cause: string(c.Cause), Reason: c.Reason, Function: c.Function, Location: c.Location,
			Thread: int64(c.Thread), Created: c.Created, WokenBy: int64(c.WokenBy), WokenByFunction: c.WokenByFunction, WokenByLocation: c.WokenByLocation}
		if c.HasProc {
			sc.Proc = ptr(int64(c.Proc))
		}
		res.Changes = append(res.Changes, sc)
	}
	write(rw, r, res)
}

type creatorJSON struct {
	Creator    string          `json:"creator"`
	Goroutines int             `json:"goroutines"`
	MainCause  string          `json:"mainCause"`
	Breakdown  []breakdownJSON `json:"breakdown"`
	Sample     []int64         `json:"sample"`
	Diagnosis  string          `json:"diagnosis"`
}

type summaryResponse struct {
	Service      string            `json:"service"`
	Instance     string            `json:"instance"`
	Period       periodJSON        `json:"period"`
	ComputedFrom computedFromJSON  `json:"computedFrom"`
	Creators     []creatorJSON     `json:"creators"`
	NextCursor   string            `json:"nextCursor,omitempty"`
	Unavailable  []unavailableJSON `json:"unavailable"`
	SchedulerURL string            `json:"schedulerUrl"`
}

type GetGoroutineSummaryHandler struct {
	uc  *usecase.SummarizeGoroutinesUsecase
	gui domain.GUI
}

func NewGetGoroutineSummaryHandler(uc *usecase.SummarizeGoroutinesUsecase, gui domain.GUI) *GetGoroutineSummaryHandler {
	return &GetGoroutineSummaryHandler{uc: uc, gui: gui}
}

func (h *GetGoroutineSummaryHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	p := pathInstance(r)
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
	out, err := h.uc.Exec(r.Context(), usecase.SummarizeGoroutinesUsecaseInput{Service: p.service, Instance: p.instance, From: from, To: to,
		Cursor: domain.PageCursor(q.Get("cursor")), MaxBytes: maxBytes, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	pr := rangeQuery(out.Period.From, out.Period.To)
	res := summaryResponse{Service: string(p.service), Instance: string(p.instance), Period: toPeriod(out.Period),
		ComputedFrom: computedFromJSON{Source: string(out.Source), From: utc(out.ComputedFrom.From), To: utc(out.ComputedFrom.To), MinuteAligned: out.MinuteAligned},
		Creators:     []creatorJSON{}, NextCursor: string(out.Next), Unavailable: toUnavailable(out.Unavailable),
		SchedulerURL: h.gui.Scheduler(p.service, p.instance, out.Period.From, out.Period.To)}
	for _, c := range out.Summaries {
		b := c.Breakdown
		cj := creatorJSON{Creator: c.Creator, Goroutines: c.Goroutines, MainCause: string(c.Main), Breakdown: toBreakdown(&b), Sample: []int64{}}
		for _, g := range c.Sample {
			cj.Sample = append(cj.Sample, int64(g))
		}
		if len(c.Sample) > 0 {
			cj.Diagnosis = diagnosisPath(p.service, p.instance, c.Sample[0], pr)
		}
		res.Creators = append(res.Creators, cj)
	}
	write(rw, r, res)
}

type groupJSON struct {
	Creator         string  `json:"creator"`
	CreatorLocation string  `json:"creatorLocation"`
	Function        string  `json:"function"`
	Location        string  `json:"location"`
	State           string  `json:"state"`
	Cause           string  `json:"cause"`
	WaitReason      string  `json:"waitReason"`
	Count           int     `json:"count"`
	MaxWaiting      dur     `json:"maxWaiting"`
	Leaked          int     `json:"leaked"`
	LeakCertainty   string  `json:"leakCertainty"`
	Goroutines      []int64 `json:"goroutines"`
}

type goroutinesAtResponse struct {
	Service       string      `json:"service"`
	Instance      string      `json:"instance"`
	At            *time.Time  `json:"at"`
	Found         bool        `json:"found"`
	SnapshotAt    *time.Time  `json:"snapshotAt"`
	LeakCheckAt   *time.Time  `json:"leakCheckAt"`
	Total         int         `json:"total"`
	RetainedSince *time.Time  `json:"retainedSince"`
	Groups        []groupJSON `json:"groups"`
	NextCursor    string      `json:"nextCursor,omitempty"`
	Diagnosis     string      `json:"diagnosis"`
	AvailableData string      `json:"availableData"`
}

type GetGoroutinesHandler struct {
	uc *usecase.ListGoroutinesAtUsecase
}

func NewGetGoroutinesHandler(uc *usecase.ListGoroutinesAtUsecase) *GetGoroutinesHandler {
	return &GetGoroutinesHandler{uc: uc}
}

func (h *GetGoroutinesHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	p := pathInstance(r)
	q := r.URL.Query()
	at, err := parseTime(q, "at", now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	maxBytes, err := parseInt(q, "max-bytes")
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	out, err := h.uc.Exec(r.Context(), usecase.ListGoroutinesAtUsecaseInput{Service: p.service, Instance: p.instance, At: at, Cursor: domain.PageCursor(q.Get("cursor")), MaxBytes: maxBytes, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	ps := map[string]string{"service": string(p.service), "instance": string(p.instance)}
	res := goroutinesAtResponse{Service: string(p.service), Instance: string(p.instance), At: utc(out.At), Found: out.Found, SnapshotAt: utc(out.SnapshotAt), LeakCheckAt: utc(out.LeakCheckAt),
		Total: out.Total, RetainedSince: utc(out.RetainedSince), Groups: []groupJSON{}, NextCursor: string(out.Next),
		Diagnosis:     apiPath(serverapi.DiagnoseGoroutine, map[string]string{"service": string(p.service), "instance": string(p.instance), "goroutine": "{goroutine}"}, url.Values{"from": {rfc(out.SnapshotAt.Add(-time.Minute))}, "to": {rfc(out.SnapshotAt)}}),
		AvailableData: apiPath(serverapi.ListAvailableData, ps, rangeQuery(at.Add(-time.Hour), at.Add(time.Hour)))}
	for _, g := range out.Groups {
		gj := groupJSON{Creator: g.Creator, CreatorLocation: g.CreatorLocation, Function: g.Function, Location: g.Location, State: string(g.State), Cause: string(g.Cause),
			WaitReason: g.WaitReason, Count: g.Count, MaxWaiting: dur(g.MaxWaiting), Leaked: g.Leaked, LeakCertainty: string(g.LeakCertainty), Goroutines: []int64{}}
		for _, id := range g.Goroutines {
			gj.Goroutines = append(gj.Goroutines, int64(id))
		}
		res.Groups = append(res.Groups, gj)
	}
	write(rw, r, res)
}

type rangeJSON struct {
	From *time.Time `json:"from"`
	To   *time.Time `json:"to"`
}

func toRanges(rs []domain.TimeRange) []rangeJSON {
	out := []rangeJSON{}
	for _, r := range rs {
		out = append(out, rangeJSON{From: utc(r.From), To: utc(r.To)})
	}
	return out
}

type availableResponse struct {
	Service         string                `json:"service"`
	Instance        string                `json:"instance"`
	From            *time.Time            `json:"from"`
	To              *time.Time            `json:"to"`
	ExecutionTraces []rangeJSON           `json:"executionTraces"`
	StateBreakdowns []rangeJSON           `json:"stateBreakdowns"`
	Snapshots       []rangeJSON           `json:"snapshots"`
	LeakChecks      []*time.Time          `json:"leakChecks"`
	PreservedTraces []preservedJSON       `json:"preservedTraces"`
	RetainedSince   map[string]*time.Time `json:"retainedSince"`
}

type GetAvailableDataHandler struct {
	uc  *usecase.ListAvailableDataUsecase
	gui domain.GUI
}

func NewGetAvailableDataHandler(uc *usecase.ListAvailableDataUsecase, gui domain.GUI) *GetAvailableDataHandler {
	return &GetAvailableDataHandler{uc: uc, gui: gui}
}

func (h *GetAvailableDataHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	now := time.Now()
	p := pathInstance(r)
	from, to, err := parseRange(r.URL.Query(), now)
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	out, err := h.uc.Exec(r.Context(), usecase.ListAvailableDataUsecaseInput{Service: p.service, Instance: p.instance, From: from, To: to, Now: now})
	if err != nil {
		middleware.SetError(r, err)
		return
	}
	res := availableResponse{Service: string(p.service), Instance: string(p.instance), From: utc(out.From), To: utc(out.To),
		ExecutionTraces: toRanges(out.ExecutionTraces), StateBreakdowns: toRanges(out.StateBreakdowns), Snapshots: toRanges(out.Snapshots), LeakChecks: []*time.Time{},
		PreservedTraces: toPreserved(h.gui, p.service, p.instance, out.PreservedTraces), RetainedSince: map[string]*time.Time{}}
	for _, t := range out.LeakChecks {
		res.LeakChecks = append(res.LeakChecks, utc(t))
	}
	for k, t := range out.RetainedSince {
		res.RetainedSince[string(k)] = utc(t)
	}
	write(rw, r, res)
}
