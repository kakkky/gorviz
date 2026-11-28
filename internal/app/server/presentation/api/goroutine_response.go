package api

import (
	"fmt"
	"strings"

	"github.com/kakkky/gorviz/internal/app/server/domain"
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
	Gorviz    bool               `json:"gorviz,omitempty"`
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
	g := goroutineResponse{ID: int64(d.ID), Gorviz: d.Gorviz, Entry: d.Entry, CreatedBy: frame(d.Creator), Range: at(d.From) + " to " + at(d.To),
		MainCause: string(d.MainCause), Certainty: string(d.Certainty), Truncated: d.TraceTruncated, GUI: p.goroutine(d.ID, d.From, d.To)}
	if d.Gorviz {
		g.Note = "a goroutine of gorviz (the pprof it opened in the app), not counted in issues"
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
