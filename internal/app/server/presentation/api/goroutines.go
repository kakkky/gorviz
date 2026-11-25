package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

const stackFrames = 12

type stoppedAtResponse struct {
	Function string `json:"function"`
	Location string `json:"location"`
	In       string `json:"waitingIn,omitempty"`
	For      string `json:"for"`
	Reason   string `json:"reason,omitempty"`
}

type wakerResponse struct {
	Goroutine int64  `json:"goroutine"`
	Function  string `json:"function"`
	Location  string `json:"location"`
	Waited    string `json:"waited"`
	Times     int    `json:"times"`
	Next      string `json:"next"`
}

type leakResponse struct {
	Leaked    string `json:"leaked"`
	Certainty string `json:"certainty"`
}

type goroutineResponse struct {
	ID        int64              `json:"id"`
	Agent     bool               `json:"agent,omitempty"`
	Entry     string             `json:"entry"`
	CreatedBy string             `json:"createdBy,omitempty"`
	Range     string             `json:"range"`
	Note      string             `json:"note,omitempty"`
	Value     string             `json:"value,omitempty"`
	MainCause string             `json:"mainCause,omitempty"`
	Certainty string             `json:"certainty"`
	Breakdown string             `json:"timeByState,omitempty"`
	StoppedAt *stoppedAtResponse `json:"stoppedAt,omitempty"`
	WokenBy   []wakerResponse    `json:"wokenBy,omitempty"`
	Leak      *leakResponse      `json:"leak,omitempty"`
	Ended     string             `json:"ended,omitempty"`
	Truncated bool               `json:"traceTruncated,omitempty"`
	Stack     []string           `json:"stack,omitempty"`
	GUI       string             `json:"gui"`
}

func goroutineOf(p paths, d domain.GoroutineDetail, withStack bool) goroutineResponse {
	g := goroutineResponse{ID: int64(d.ID), Agent: d.Agent, Entry: d.Entry, CreatedBy: frame(d.Creator), Range: at(d.From) + " to " + at(d.To),
		MainCause: string(d.MainCause), Certainty: string(d.Certainty), Truncated: d.TraceTruncated, GUI: p.goroutine(d.ID, d.From, d.To)}
	if d.Agent {
		g.Note = "a goroutine of the gorviz agent, not counted in issues"
	}
	var parts []string
	for _, b := range d.Breakdown {
		parts = append(parts, string(b.Cause)+" "+dur(b.Duration))
	}
	g.Breakdown = strings.Join(parts, ", ")
	if d.StoppedAt.Function != "" {
		g.StoppedAt = &stoppedAtResponse{Function: d.StoppedAt.Function, Location: d.StoppedAt.Location(), In: d.StoppedIn, For: dur(d.StoppedFor), Reason: d.StoppedReason}
	}
	for _, w := range d.Wakers {
		g.WokenBy = append(g.WokenBy, wakerResponse{Goroutine: int64(w.G), Function: w.Function, Location: w.Location, Waited: dur(w.Total), Times: w.Count, Next: goroutineCommand(w.G, w.WaitFrom, w.WaitTo)})
	}
	if d.Leak != nil {
		g.Leak = &leakResponse{Leaked: fmt.Sprintf("%d of the %d goroutines waiting at the same place leaked", d.Leak.Count, d.Leak.Of), Certainty: string(d.Leak.Certainty)}
	}
	if d.Ended {
		g.Ended = at(d.EndedAt)
	}
	if !d.HasObservations {
		g.Note = strings.TrimPrefix(g.Note+"; no state change of this goroutine was recorded in the range", "; ")
	}
	if withStack {
		for i, f := range d.Stack {
			if i == stackFrames {
				g.Stack = append(g.Stack, fmt.Sprintf("... %d more frames", len(d.Stack)-stackFrames))
				break
			}
			g.Stack = append(g.Stack, frame(f))
		}
	}
	return g
}

type goroutineDetailResponse struct {
	Goroutine goroutineResponse `json:"goroutine"`
	Next      []string          `json:"next"`
}

// GetGoroutineHandler は 1 つの goroutine の詳細を返す
type GetGoroutineHandler struct {
	detail *usecase.GetGoroutineDetailUsecase
	paths  paths
}

func NewGetGoroutineHandler(detail *usecase.GetGoroutineDetailUsecase, guiBase string) *GetGoroutineHandler {
	return &GetGoroutineHandler{detail: detail, paths: paths{base: guiBase}}
}

func rangeInput(r *http.Request) (from, to time.Duration, has bool, err error) {
	q := r.URL.Query()
	if q.Get("from") == "" && q.Get("to") == "" {
		return 0, 0, false, nil
	}
	f, err := parseAt(q.Get("from"))
	if err != nil {
		return 0, 0, false, err
	}
	t, err := parseAt(q.Get("to"))
	if err != nil {
		return 0, 0, false, err
	}
	return f, t, true, nil
}

func (h *GetGoroutineHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		middleware.Fail(r, domain.ErrInvalidArgument.With(fmt.Sprintf("goroutine id %q is not a number", r.PathValue("id"))))
		return
	}
	from, to, has, err := rangeInput(r)
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	out, err := h.detail.Exec(r.Context(), usecase.GetGoroutineDetailUsecaseInput{ID: domain.GoroutineID(id), From: from, To: to, HasRange: has})
	if err != nil {
		middleware.Fail(r, err)
		return
	}
	resp := goroutineDetailResponse{Goroutine: goroutineOf(h.paths, out.Detail, true)}
	for _, w := range out.Detail.Wakers {
		resp.Next = append(resp.Next, goroutineCommand(w.G, w.WaitFrom, w.WaitTo))
	}
	resp.Next = append(resp.Next, "gorviz docs goroutines")
	if err := write(rw, r, resp, 0); err != nil {
		middleware.Fail(r, err)
	}
}
