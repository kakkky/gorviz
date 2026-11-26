package api

import (
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/server"
)

type loadResponse struct {
	Phase       string `json:"phase"`
	Concurrency int    `json:"concurrency"`
	Requests    int    `json:"requests"`
	RPS         string `json:"rps"`
	P50         string `json:"p50"`
	P90         string `json:"p90"`
	P99         string `json:"p99"`
	Max         string `json:"max"`
	Statuses    string `json:"statuses"`
	Canceled    string `json:"canceled,omitempty"`
}

func loadOf(s domain.LoadStats) loadResponse {
	var parts []string
	for _, c := range s.StatusBreakdown() {
		parts = append(parts, c.Status+":"+strconv.Itoa(c.Count))
	}
	r := loadResponse{Phase: s.Phase, Concurrency: s.Concurrency, Requests: s.Requests, RPS: fmt.Sprintf("%.0f", s.RPS()),
		P50: dur(s.Latency.Quantile(0.5)), P90: dur(s.Latency.Quantile(0.9)), P99: dur(s.Latency.Quantile(0.99)), Max: dur(s.Latency.Quantile(1)), Statuses: strings.Join(parts, " ")}
	if s.Canceled > 0 && s.Requests > 0 {
		r.Canceled = fmt.Sprintf("%d (%.0f%%)", s.Canceled, float64(s.Canceled)*100/float64(s.Requests))
	}
	return r
}

type stepResponse struct {
	Request string `json:"request"`
	Result  string `json:"result"`
}

type smokeResponse struct {
	Flow   string         `json:"flow"`
	Passed bool           `json:"passed"`
	Steps  []stepResponse `json:"steps"`
}

func smokeOf(rs []domain.SmokeResult) []smokeResponse {
	out := make([]smokeResponse, 0, len(rs))
	for _, r := range rs {
		s := smokeResponse{Flow: r.Flow, Passed: r.Passed}
		for _, st := range r.Steps {
			res := strconv.Itoa(st.Status)
			if st.Err != "" {
				res = "error: " + st.Err
			}
			if !st.Passed {
				res += " (failed)"
			}
			s.Steps = append(s.Steps, stepResponse{Request: st.Method + " " + st.Path, Result: res})
		}
		out = append(out, s)
	}
	return out
}

type functionCoverageResponse struct {
	Function  string `json:"function"`
	Covered   string `json:"covered"`
	Uncovered string `json:"uncoveredLines"`
}

type coverageResponse struct {
	Covered   string                     `json:"covered"`
	Partial   []functionCoverageResponse `json:"partlyCovered,omitempty"`
	Unreached []string                   `json:"notReached,omitempty"`
	Omitted   string                     `json:"omitted,omitempty"`
}

const coverageBudget = 4 << 10

func coverageOf(c *domain.CoverageReport) *coverageResponse {
	if c == nil {
		return nil
	}
	r := &coverageResponse{Covered: fmt.Sprintf("%.1f%% of %d statements", c.Percent, c.Statements)}
	size, omitted := 0, 0
	for _, f := range c.Partial {
		var lines []string
		for _, l := range f.Uncovered {
			lines = append(lines, l.String())
		}
		item := functionCoverageResponse{Function: f.Function, Covered: fmt.Sprintf("%.0f%%", f.Percent), Uncovered: path.Base(f.File) + ":" + strings.Join(lines, ",")}
		size += len(item.Function) + len(item.Uncovered) + 16
		if size > coverageBudget {
			omitted++
			continue
		}
		r.Partial = append(r.Partial, item)
	}
	for _, f := range c.Unreached {
		size += len(f) + 4
		if size > coverageBudget {
			omitted++
			continue
		}
		r.Unreached = append(r.Unreached, f)
	}
	if omitted > 0 {
		r.Omitted = fmt.Sprintf("%d more functions are not shown", omitted)
	}
	return r
}

type summaryResponse struct {
	Range        rangeResponse        `json:"range"`
	Loaded       bool                 `json:"loaded"`
	Stopped      string               `json:"stopped,omitempty"`
	Smoke        string               `json:"smoke,omitempty"`
	SmokeFlows   []smokeResponse      `json:"smokeFlows,omitempty"`
	RampUp       []loadResponse       `json:"rampUp,omitempty"`
	Cancellation *loadResponse        `json:"cancellation,omitempty"`
	Coverage     *coverageResponse    `json:"coverage,omitempty"`
	AppExitCode  *int                 `json:"appExitCode,omitempty"`
	Issues       []diagnosedIssueBody `json:"issues"`
	ExitCode     int                  `json:"exitCode"`
	GUI          string               `json:"gui"`
	Next         []string             `json:"next"`
}

func summaryOf(s domain.DiagnosisSummary, p paths) summaryResponse {
	r := summaryResponse{Range: rangeResponse{From: at(s.From), To: at(s.To)}, Loaded: s.Loaded, Stopped: s.Stopped, Issues: []diagnosedIssueBody{}, ExitCode: s.ExitCode, GUI: p.base}
	if s.Loaded {
		passed, total := domain.SmokePassed(s.Smoke)
		r.Smoke = fmt.Sprintf("%d of %d flows passed", passed, total)
		if passed < total {
			r.SmokeFlows = smokeOf(s.Smoke)
		}
	}
	for _, l := range s.Levels {
		r.RampUp = append(r.RampUp, loadOf(l))
	}
	if s.Cancellation != nil {
		c := loadOf(*s.Cancellation)
		r.Cancellation = &c
	}
	r.Coverage = coverageOf(s.Coverage)
	if s.AppExited {
		code := s.AppExitCode
		r.AppExitCode = &code
	}
	for _, is := range s.Issues {
		r.Issues = append(r.Issues, diagnosedIssueOf(is))
		if len(r.Next) < 3 {
			r.Next = append(r.Next, "gorviz issue "+string(is.ID))
		}
	}
	switch {
	case s.Stopped != "":
		r.Next = append(r.Next, "gorviz docs scenario")
	case len(s.Issues) == 0:
		r.Next = append(r.Next, "check the coverage to see whether the scenario reached the code you care about")
	}
	return r
}

// PostDiagnoseHandler はシナリオ (本文。空ならシナリオなし) で診断し、phase が終わるたびに 1 つずつ流し、最後に要約を返す。終了コードは trailer で返す
type PostDiagnoseHandler struct {
	diagnose *usecase.DiagnoseAppUsecase
	paths    paths
}

func NewPostDiagnoseHandler(diagnose *usecase.DiagnoseAppUsecase, guiBase string) *PostDiagnoseHandler {
	return &PostDiagnoseHandler{diagnose: diagnose, paths: paths{base: guiBase}}
}

func (h *PostDiagnoseHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	scenario, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		middleware.Fail(r, domain.ErrInvalidArgument.Wrap(err, "read the scenario"))
		return
	}
	started := false
	flusher, _ := rw.(http.Flusher)
	emit := func(v any) {
		if !started {
			_, media, _ := encode(r, struct{}{})
			rw.Header().Set("Content-Type", media)
			rw.Header().Set("Trailer", server.HeaderExitCode)
			started = true
		}
		b, _, err := encode(r, v)
		if err != nil {
			return
		}
		_, _ = rw.Write(b)
		if !wantsJSON(r) {
			_, _ = rw.Write([]byte("\n"))
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	out, err := h.diagnose.Exec(r.Context(), usecase.DiagnoseAppUsecaseInput{Scenario: scenario, Progress: func(p domain.DiagnosisProgress) {
		emit(map[string]any{"progress": progressOf(p)})
	}})
	if err != nil {
		if !started {
			middleware.Fail(r, err)
			return
		}
		emit(errorBody{Error: err.Error(), Docs: "gorviz docs"})
		rw.Header().Set(server.HeaderExitCode, strconv.Itoa(domain.ExitNotDiagnosed))
		return
	}
	emit(map[string]any{"summary": summaryOf(out.Summary, h.paths)})
	rw.Header().Set(server.HeaderExitCode, strconv.Itoa(out.Summary.ExitCode))
}

type progressResponse struct {
	Phase string          `json:"phase"`
	Smoke string          `json:"smoke,omitempty"`
	Flows []smokeResponse `json:"flows,omitempty"`
	Load  *loadResponse   `json:"load,omitempty"`
	Note  string          `json:"note,omitempty"`
}

func progressOf(p domain.DiagnosisProgress) progressResponse {
	r := progressResponse{Phase: p.Phase, Note: p.Note}
	if p.Smoke != nil {
		passed, total := domain.SmokePassed(p.Smoke)
		r.Smoke = fmt.Sprintf("%d of %d flows passed", passed, total)
		r.Flows = smokeOf(p.Smoke)
	}
	if p.Load != nil {
		l := loadOf(*p.Load)
		r.Load = &l
	}
	return r
}
