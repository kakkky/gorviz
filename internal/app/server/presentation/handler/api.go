package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/api/serverapi"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// API は CLI・MCP が server と話すための JSON の口。画面と同じ診断を返す。
// どの agent のものかは ?agent= で選び、なければ最初につながった agent を使う
type API struct {
	resolve func(r *http.Request) (*usecase.UseCase, bool)
	agents  func() []serverapi.AgentJSON
}

func NewAPI(resolve func(r *http.Request) (*usecase.UseCase, bool), agents func() []serverapi.AgentJSON) *API {
	return &API{resolve: resolve, agents: agents}
}

func (a *API) Routes() []Route {
	return []Route{
		{Pattern: "GET /api/agents", Handler: http.HandlerFunc(a.listAgents)},
		{Pattern: "GET /api/overview", Handler: a.withUseCase(a.overview)},
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

func (a *API) overview(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	out, err := uc.NewGetRuntimeSnapshotUsecase().Exec(r.Context(), usecase.GetRuntimeSnapshotUsecaseInput{})
	if err != nil {
		writeJSONError(rw, http.StatusServiceUnavailable, "no snapshot yet: the agent has not sent a goroutine dump")
		return
	}
	s := out.Snapshot
	o := serverapi.OverviewJSON{
		At: s.At, DumpAt: s.DumpAt, Goroutines: s.GoroutineCount, States: map[string]int{}, GoroutineHistory: s.GoroutineHistory,
		HeapObjectsBytes: s.Runtime.HeapObjectsBytes, HeapGoalBytes: s.Runtime.HeapGoalBytes,
		GCPerSecond: s.Runtime.GCPerSecond, CPUWaiting: s.Runtime.CPUWaiting,
	}
	for k, v := range s.StateCounts {
		o.States[string(k)] = v
	}
	for _, t := range s.TopOrigins {
		o.TopOrigins = append(o.TopOrigins, serverapi.OriginJSON{Origin: string(t.Origin), Count: t.Count})
	}
	if s.HasRecording {
		o.Recording = &s.Recording
	}
	if issues, err := uc.NewListIssuesUsecase().Exec(r.Context(), usecase.ListIssuesUsecaseInput{Now: time.Now()}); err == nil {
		o.Issues = len(issues.Issues)
	}
	writeJSON(rw, o)
}

func (a *API) issues(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	out, err := uc.NewListIssuesUsecase().Exec(r.Context(), usecase.ListIssuesUsecaseInput{Now: time.Now()})
	if err != nil {
		writeJSONError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	issues := []serverapi.IssueJSON{}
	for _, i := range out.Issues {
		j := serverapi.IssueJSON{Kind: string(i.Kind), Certainty: string(i.Certainty), Subject: i.Subject, Summary: i.Summary, Location: i.Location, Trend: i.Trend, At: i.At, From: i.From}
		if i.HasGoroutine {
			g := int64(i.Goroutine)
			j.Goroutine = &g
			j.GoroutineQuery = fmt.Sprintf("at=%d&from=%d", i.At.UnixNano(), i.From.UnixNano())
		}
		issues = append(issues, j)
	}
	writeJSON(rw, issues)
}

func (a *API) logs(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	q := r.URL.Query()
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 || limit > 1000 {
		limit = 100
	}
	in := usecase.ListLogsUsecaseInput{Query: q.Get("q"), Level: model.LogLevel(q.Get("level")), Limit: limit}
	in.From, _ = unixNanoParam(r, "from")
	in.To, _ = unixNanoParam(r, "to")
	out, err := uc.NewListLogsUsecase().Exec(r.Context(), in)
	if err != nil {
		writeJSONError(rw, http.StatusInternalServerError, err.Error())
		return
	}
	logs := []serverapi.LogJSON{}
	for _, l := range out.Lines {
		logs = append(logs, serverapi.LogJSON{ID: int64(l.Line.ID), At: l.Line.At, Level: string(l.Line.Level), Message: l.Line.Message, Duration: formatDuration(l.Line), TraceLinked: l.Line.HasTraceSeq, Raw: l.Line.Raw})
	}
	writeJSON(rw, logs)
}

func (a *API) goroutine(rw http.ResponseWriter, r *http.Request, uc *usecase.UseCase) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	at, atErr := unixNanoParam(r, "at")
	if err != nil || atErr != nil {
		writeJSONError(rw, http.StatusBadRequest, "invalid goroutine id or at (unix nanoseconds)")
		return
	}
	in := usecase.GetGoroutineUsecaseInput{Goroutine: model.GoroutineID(id), At: at, From: at.Add(-goroutineReplayHalf), To: at.Add(goroutineReplayHalf), ReplayHalf: goroutineReplayHalf}
	from, fromErr := unixNanoParam(r, "from")
	if fromErr == nil && from.Before(at) {
		in.From, in.To = from, at
	}
	out, err := uc.NewGetGoroutineUsecase().Exec(r.Context(), in)
	if err != nil {
		writeJSONError(rw, http.StatusBadRequest, err.Error())
		return
	}
	j := serverapi.GoroutineJSON{
		Goroutine: id, Entry: out.Entry, From: out.From, To: out.To, At: out.At, Leaked: out.HasDump && out.Dump.Leaked,
		Function: out.Function, Location: out.Location, WaitReason: out.WaitReason, Breakdown: map[string]string{},
		Unrecorded: out.Breakdown.Unrecorded.String(),
	}
	if fromErr != nil {
		from = time.Time{}
	}
	j.EvidenceURL = goroutineURL(model.GoroutineID(id), at, from)
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
		recs = []model.SavedRecording{}
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
