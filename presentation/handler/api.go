package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/usecase"
)

// API は TUI・CLI・MCP が server と話すための JSON の口。画面と同じ診断を返す。
// どの agent のものかは ?agent= で選び、なければ最初につながった agent を使う
type API struct {
	resolve func(r *http.Request) (*usecase.UseCase, bool)
	agents  func() []AgentJSON
}

type AgentJSON struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Connected bool   `json:"connected"`
}

func NewAPI(resolve func(r *http.Request) (*usecase.UseCase, bool), agents func() []AgentJSON) *API {
	return &API{resolve: resolve, agents: agents}
}

func (a *API) Routes() []Route {
	return []Route{
		{Pattern: "GET /api/agents", Handler: http.HandlerFunc(a.listAgents)},
		{Pattern: "GET /api/overview", Handler: a.withUseCase(a.overview)},
		{Pattern: "GET /api/scheduler", Handler: a.withUseCase(a.scheduler)},
		{Pattern: "GET /api/issues", Handler: a.withUseCase(a.issues)},
		{Pattern: "GET /api/logs", Handler: a.withUseCase(a.logs)},
		{Pattern: "GET /api/goroutines/{id}", Handler: a.withUseCase(a.goroutine)},
		{Pattern: "GET /api/recordings", Handler: a.withUseCase(a.recordings)},
		{Pattern: "GET /api/recordings/{id}/chunks/{n}", Handler: a.withUseCase(a.recordingChunk)},
	}
}

func (a *API) withUseCase(f func(http.ResponseWriter, *http.Request, *usecase.UseCase)) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		uc, ok := a.resolve(r)
		if !ok {
			writeJSONError(rw, http.StatusNotFound, "no agent is connected (or the requested agent is unknown)")
			return
		}
		f(rw, r, uc)
	})
}

func (a *API) listAgents(rw http.ResponseWriter, _ *http.Request) {
	writeJSON(rw, a.agents())
}

type OverviewJSON struct {
	At               time.Time             `json:"at"`
	DumpAt           time.Time             `json:"dumpAt"`
	Goroutines       int                   `json:"goroutines"`
	States           map[string]int        `json:"states"`
	GoroutineHistory []int                 `json:"goroutineHistory"`
	TopOrigins       []OriginJSON          `json:"topOrigins"`
	Scopes           []domain.ScopeSummary `json:"scopes"`
	HeapLiveBytes    uint64                `json:"heapLiveBytes"`
	HeapGoalBytes    uint64                `json:"heapGoalBytes"`
	GCPerSecond      float64               `json:"gcPerSecond"`
	CPUWaiting       float64               `json:"cpuWaiting"`
	Issues           int                   `json:"issues"`
	ErrorsLastMinute int                   `json:"errorsLastMinute"`
	SlowLastMinute   int                   `json:"slowLastMinute"`
	Recording        *domain.RecordingSpan `json:"recording,omitempty"`
}

type OriginJSON struct {
	Origin string `json:"origin"`
	Count  int    `json:"count"`
}

func (a *API) overview(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	out, err := uc.NewGetRuntimeSnapshotUsecase().Exec(r.Context(), usecase.GetRuntimeSnapshotUsecaseInput{})
	if err != nil {
		writeJSONError(rw, http.StatusServiceUnavailable, "no snapshot yet: the agent has not sent a goroutine dump")
		return
	}
	s := out.Snapshot
	o := OverviewJSON{
		At: s.At, DumpAt: s.DumpAt, Goroutines: s.GoroutineCount, States: map[string]int{}, GoroutineHistory: s.GoroutineHistory,
		Scopes: s.Scopes, HeapLiveBytes: s.Runtime.HeapLiveBytes, HeapGoalBytes: s.Runtime.HeapGoalBytes,
		GCPerSecond: s.Runtime.GCPerSecond, CPUWaiting: s.Runtime.CPUWaiting,
	}
	for k, v := range s.StateCounts {
		o.States[string(k)] = v
	}
	for _, t := range s.TopOrigins {
		o.TopOrigins = append(o.TopOrigins, OriginJSON{Origin: string(t.Origin), Count: t.Count})
	}
	if s.HasRecording {
		o.Recording = &s.Recording
	}
	if issues, err := uc.NewListIssuesUsecase().Exec(r.Context(), usecase.ListIssuesUsecaseInput{Now: time.Now()}); err == nil {
		o.Issues, o.ErrorsLastMinute, o.SlowLastMinute = len(issues.Issues), issues.ErrorsLastMinute, issues.SlowLastMinute
	}
	writeJSON(rw, o)
}

// SchedulerJSON は最新の観測窓の終わりの GMP。run queue の中身は trace からの推測
type SchedulerJSON struct {
	WindowStartedAt time.Time                `json:"windowStartedAt"`
	WindowEnd       time.Time                `json:"windowEnd"`
	Procs           []ProcJSON               `json:"procs"`
	GlobalRunQueue  []int64                  `json:"globalRunQueue"`
	Waiting         []WaitingJSON            `json:"waiting"`
	HandedOff       []int64                  `json:"handedOff"`
	Exited          int                      `json:"exitedInWindow"`
	Lanes           []ProcLaneJSON           `json:"lanes"`
	SchedTrace      *domain.SchedTraceReport `json:"schedtrace,omitempty"`
}

type ProcJSON struct {
	Proc             int64   `json:"proc"`
	Status           string  `json:"status"`
	Thread           int64   `json:"thread"`
	Running          int64   `json:"running"`
	RunningEntry     string  `json:"runningEntry"`
	RunningInSyscall bool    `json:"runningInSyscall"`
	LocalRunQueue    []int64 `json:"localRunQueue"`
}

type WaitingJSON struct {
	Entry      string  `json:"entry"`
	Reason     string  `json:"reason"`
	Goroutines []int64 `json:"goroutines"`
}

type ProcLaneJSON struct {
	Proc        int64     `json:"proc"`
	Utilization float64   `json:"utilization"`
	Busy        []float64 `json:"busy"`
	Goroutines  []int64   `json:"goroutines"`
}

func (a *API) scheduler(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	out, err := uc.NewGetRuntimeSnapshotUsecase().Exec(r.Context(), usecase.GetRuntimeSnapshotUsecaseInput{})
	if err != nil {
		writeJSONError(rw, http.StatusServiceUnavailable, "no snapshot yet")
		return
	}
	tl := out.Snapshot.Timeline
	st := tl.StateAt(tl.Window)
	s := SchedulerJSON{WindowStartedAt: out.Snapshot.TimelineStartedAt, WindowEnd: out.Snapshot.TimelineStartedAt.Add(tl.Window), Exited: len(st.Dead)}
	for _, p := range st.Procs {
		pj := ProcJSON{Proc: int64(p.Proc), Status: string(p.Status), Thread: -1, Running: -1, RunningInSyscall: p.RunningInSyscall, LocalRunQueue: ids(p.LocalQueue)}
		if p.HasThread {
			pj.Thread = int64(p.Thread)
		}
		if p.HasRunning {
			pj.Running, pj.RunningEntry = int64(p.Running), tl.Entries[p.Running]
		}
		s.Procs = append(s.Procs, pj)
	}
	s.GlobalRunQueue = ids(st.GlobalQueue)
	for key, gs := range st.Waiting {
		s.Waiting = append(s.Waiting, WaitingJSON{Entry: key.Entry, Reason: key.Reason, Goroutines: ids(gs)})
	}
	for _, h := range st.HandedOff {
		s.HandedOff = append(s.HandedOff, int64(h.Goroutine))
	}
	for _, l := range tl.SortedProcs() {
		lane := ProcLaneJSON{Proc: int64(l.Proc), Utilization: l.Utilization(tl.Window)}
		for _, b := range l.Buckets(tl.Window, 60) {
			lane.Busy = append(lane.Busy, b.Busy)
			lane.Goroutines = append(lane.Goroutines, int64(b.Goroutine))
		}
		s.Lanes = append(s.Lanes, lane)
	}
	if st, err := uc.NewGetSchedTraceUsecase().Exec(r.Context(), usecase.GetSchedTraceUsecaseInput{}); err == nil && st.Available {
		s.SchedTrace = &st.Report
	}
	writeJSON(rw, s)
}

type IssueJSON struct {
	Kind      string    `json:"kind"`
	Certainty string    `json:"certainty"`
	Subject   string    `json:"subject"`
	Summary   string    `json:"summary"`
	Location  string    `json:"location,omitempty"`
	Trend     []int     `json:"trend,omitempty"`
	Goroutine *int64    `json:"goroutine,omitempty"`
	At        time.Time `json:"at"`
	From      time.Time `json:"from"`
	// GoroutineQuery は /api/goroutines/{goroutine} に付ける問い合わせ (at / from)。代表の G を診断するのに使う
	GoroutineQuery string `json:"goroutineQuery,omitempty"`
	LogQuery       string `json:"logQuery,omitempty"`
}

func (a *API) issues(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	out, err := uc.NewListIssuesUsecase().Exec(r.Context(), usecase.ListIssuesUsecaseInput{Now: time.Now()})
	if err != nil {
		writeJSONError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	issues := []IssueJSON{}
	for _, i := range out.Issues {
		j := IssueJSON{Kind: string(i.Kind), Certainty: string(i.Certainty), Subject: i.Subject, Summary: i.Summary, Location: i.Location, Trend: i.Trend, At: i.At, From: i.From, LogQuery: i.LogQuery}
		if i.HasGoroutine {
			g := int64(i.Goroutine)
			j.Goroutine = &g
			j.GoroutineQuery = fmt.Sprintf("at=%d&from=%d", i.At.UnixNano(), i.From.UnixNano())
		}
		issues = append(issues, j)
	}
	writeJSON(rw, issues)
}

type LogJSON struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Level    string    `json:"level"`
	Message  string    `json:"message"`
	Duration string    `json:"duration,omitempty"`
	// TraceLinked はログを書いた G を slogtrace の番号で特定できる (FACT) 行か。/logs/{id}/replay の画面で確かめられる
	TraceLinked bool   `json:"traceLinked"`
	Raw         string `json:"raw"`
}

func (a *API) logs(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	q := r.URL.Query()
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 || limit > 1000 {
		limit = 100
	}
	in := usecase.ListLogsUsecaseInput{Query: q.Get("q"), Level: domain.LogLevel(q.Get("level")), Limit: limit}
	in.From, _ = unixNanoParam(r, "from")
	in.To, _ = unixNanoParam(r, "to")
	out, err := uc.NewListLogsUsecase().Exec(r.Context(), in)
	if err != nil {
		writeJSONError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	logs := []LogJSON{}
	for _, l := range out.Lines {
		logs = append(logs, LogJSON{ID: int64(l.Line.ID), At: l.Line.At, Level: string(l.Line.Level), Message: l.Line.Message, Duration: formatDuration(l.Line), TraceLinked: l.Line.HasTraceSeq, Raw: l.Line.Raw})
	}
	writeJSON(rw, logs)
}

type GoroutineJSON struct {
	Goroutine  int64             `json:"goroutine"`
	Entry      string            `json:"entry"`
	From       time.Time         `json:"from"`
	To         time.Time         `json:"to"`
	At         time.Time         `json:"at"`
	Leaked     bool              `json:"leaked"`
	Primary    string            `json:"primaryCause,omitempty"`
	Certainty  string            `json:"certainty,omitempty"`
	Function   string            `json:"stoppedIn,omitempty"`
	Location   string            `json:"location,omitempty"`
	WaitReason string            `json:"waitReason,omitempty"`
	CreatedBy  string            `json:"createdBy,omitempty"`
	Breakdown  map[string]string `json:"timeByState"`
	Unrecorded string            `json:"unrecorded"`
	// EvidenceURL は人間が根拠 (GMP 再生) を画面で確かめるための URL
	EvidenceURL string `json:"evidenceUrl"`
}

func (a *API) goroutine(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	at, atErr := unixNanoParam(r, "at")
	if err != nil || atErr != nil {
		writeJSONError(rw, http.StatusBadRequest, "invalid goroutine id or at (unix nanoseconds)")
		return
	}
	in := usecase.GetGoroutineUsecaseInput{Goroutine: domain.GoroutineID(id), At: at, From: at.Add(-goroutineReplayHalf), To: at.Add(goroutineReplayHalf), ReplayHalf: goroutineReplayHalf}
	from, fromErr := unixNanoParam(r, "from")
	if fromErr == nil && from.Before(at) {
		in.From, in.To = from, at
	}
	out, err := uc.NewGetGoroutineUsecase().Exec(r.Context(), in)
	if err != nil {
		writeJSONError(rw, http.StatusBadRequest, err.Error())
		return
	}
	j := GoroutineJSON{
		Goroutine: id, Entry: out.Entry, From: out.From, To: out.To, At: out.At, Leaked: out.HasDump && out.Dump.Leaked,
		Function: out.Function, Location: out.Location, WaitReason: out.WaitReason, Breakdown: map[string]string{},
		Unrecorded: out.Breakdown.Unrecorded.String(),
	}
	if fromErr != nil {
		from = time.Time{}
	}
	j.EvidenceURL = goroutineURL(domain.GoroutineID(id), at, from)
	if out.HasPrimary {
		j.Primary, j.Certainty = string(out.Primary), string(out.Certainty)
	}
	if out.HasDump && out.Dump.CreatedBy != "" {
		j.CreatedBy = string(out.Dump.Origin())
	}
	for c, d := range out.Breakdown.Durations {
		j.Breakdown[string(c)] = d.String()
	}
	writeJSON(rw, j)
}

func (a *API) recordings(rw http.ResponseWriter, _ *http.Request, uc *usecase.UseCase) {
	recs := uc.ListRecordings()
	if recs == nil {
		recs = []domain.SavedRecording{}
	}
	writeJSON(rw, recs)
}

// recordingChunk は保存した録画の n 番目の区切りを、取ったままの trace として返す。`go tool trace` で開ける
func (a *API) recordingChunk(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	chunks, ok := uc.RecordingChunks(r.PathValue("id"))
	n, err := strconv.Atoi(r.PathValue("n"))
	if !ok || err != nil || n < 0 || n >= len(chunks) {
		writeJSONError(rw, http.StatusNotFound, "no such recording chunk")
		return
	}
	rw.Header().Set("Content-Type", "application/octet-stream")
	if _, err := rw.Write(chunks[n].Raw); err != nil {
		slog.ErrorContext(r.Context(), "write a recording chunk", slog.Any("err", err))
	}
}

func ids(gs []domain.GoroutineID) []int64 {
	out := make([]int64, len(gs))
	for i, g := range gs {
		out[i] = int64(g)
	}
	return out
}

func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(rw)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		slog.Error("encode json", slog.Any("err", err))
	}
}

func writeJSONError(rw http.ResponseWriter, status int, msg string) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(map[string]string{"error": msg})
}
