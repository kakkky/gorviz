package web

import (
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// maxReplayEvents は 1 つの窓で送る G の状態の変化の上限。ブラウザが重くなりすぎないようにする
const maxReplayEvents = 300000

type table struct {
	index map[string]int
	names []string
}

func (t *table) of(s string) int {
	if s == "" {
		return -1
	}
	if i, ok := t.index[s]; ok {
		return i
	}
	if t.index == nil {
		t.index = map[string]int{}
	}
	t.index[s] = len(t.names)
	t.names = append(t.names, s)
	return t.index[s]
}

func indexOf[T comparable](list []T, v T) int {
	return slices.Index(list, v)
}

// replayWindow は再生する窓の JSON。チャンクの中のイベントを、関数名と待機理由の表と番号に詰める
func replayWindow(out usecase.ListTraceChunksUsecaseOutput) string {
	c := out.Chunk
	var fns, reasons table
	w := components.ReplayWindow{From: rfc(c.StartedAt), To: rfc(c.EndedAt()), StartedAtMs: float64(c.StartedAt.UnixNano()) / 1e6, DurationNs: int64(c.Duration),
		Entries: map[string]int{}, Events: []components.ReplayEvent{}, ProcEvents: []components.ReplayProc{}, Lanes: [][]components.ReplaySpan{},
		STW: []components.ReplayRange{}, GCMark: []components.ReplayRange{}, Leaks: []components.ReplayLeak{}, LeakCheckAt: rfc(out.LeakCheckAt), FromPreserved: out.FromPreserved}
	procs := map[observation.ProcID]bool{}
	for _, ev := range c.Events {
		switch ev.Kind {
		case observation.StateChangeGoroutine:
			if len(w.Events) >= maxReplayEvents {
				w.Truncated = true
				continue
			}
			e := components.ReplayEvent{At: int64(ev.At), G: int64(ev.Goroutine), Place: indexOf(components.ReplayPlaceKinds, ev.Place.Kind), Proc: -1, Thread: int64(ev.Place.Thread),
				Reason: reasons.of(ev.Place.Reason), Function: fns.of(ev.Function), Cause: -1, WokenBy: int64(ev.WokenBy)}
			if ev.Place.HasProc {
				e.Proc = int64(ev.Place.Proc)
				procs[ev.Place.Proc] = true
			}
			if cause, ok := domain.CauseOfPlace(ev.Place, ev.Function); ok {
				e.Cause = indexOf(domain.MainCauses, cause)
			}
			w.Events = append(w.Events, e)
		case observation.StateChangeProc:
			p := components.ReplayProc{At: int64(ev.At), Proc: int64(ev.Proc), Running: ev.ProcStatus == observation.ProcRunning, Thread: -1}
			if p.Running {
				p.Thread = int64(ev.Thread)
			}
			procs[ev.Proc] = true
			w.ProcEvents = append(w.ProcEvents, p)
		}
	}
	lanes := map[observation.ProcID][]components.ReplaySpan{}
	for _, l := range c.Procs {
		procs[l.Proc] = true
		for _, s := range l.Spans {
			lanes[l.Proc] = append(lanes[l.Proc], components.ReplaySpan{Start: int64(s.Start), End: int64(s.End), G: int64(s.Goroutine)})
		}
	}
	for p := range procs {
		w.Procs = append(w.Procs, int64(p))
	}
	slices.Sort(w.Procs)
	for _, p := range w.Procs {
		spans := lanes[observation.ProcID(p)]
		if spans == nil {
			spans = []components.ReplaySpan{}
		}
		w.Lanes = append(w.Lanes, spans)
	}
	for _, r := range c.GC {
		switch r.Kind {
		case observation.GCStopTheWorld:
			w.STW = append(w.STW, components.ReplayRange{Start: int64(r.Start), End: int64(r.End), Reason: reasons.of(r.Reason), ByGC: r.ByGC()})
		case observation.GCMark:
			w.GCMark = append(w.GCMark, components.ReplayRange{Start: int64(r.Start), End: int64(r.End), Reason: -1, ByGC: true})
		}
	}
	for g, e := range c.Entries {
		w.Entries[strconv.FormatInt(int64(g), 10)] = fns.of(e)
	}
	for _, m := range out.Leaks {
		for _, g := range m.Goroutines {
			w.Leaks = append(w.Leaks, components.ReplayLeak{G: int64(g), Certainty: m.Certainty, Count: m.Count, Of: m.Of})
		}
	}
	w.Functions, w.Reasons = orEmpty(fns.names), orEmpty(reasons.names)
	b, err := json.Marshal(w)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func propsJSON(p components.GMPReplayProps) string {
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(b)
}

var unavailableFields = map[string]string{
	"schedulerReplay": "scheduler replay",
	"executionTrace":  "execution trace",
	"breakdown":       "time by state (per minute)",
	"leak":            "leak",
}

func unavailableViews(us []usecase.Unavailable) []components.UnavailableViewModel {
	out := make([]components.UnavailableViewModel, 0, len(us))
	for _, u := range us {
		field := unavailableFields[u.Field]
		if field == "" {
			field = u.Field
		}
		text := "no data is recorded for this period"
		if u.Reason == usecase.ReasonPastRetention {
			text = pastRetention
		}
		out = append(out, components.UnavailableViewModel{Field: field, Text: text, Since: dateTime(u.RetainedSince)})
	}
	return out
}

func nearbyLinks(s observation.ServiceName, i observation.InstanceName, ps []domain.PreservedTrace) []components.LinkItemViewModel {
	out := make([]components.LinkItemViewModel, 0, len(ps))
	for _, p := range ps {
		note := "kept around an error log"
		if p.Kind == domain.PreservedAroundIssue {
			note = "kept around an issue occurrence"
		}
		out = append(out, components.LinkItemViewModel{Label: span(p.From, p.To), URL: paths.Scheduler(s, i, p.From, p.To), Note: note + " · " + p.Dir})
	}
	return out
}

func shiftQuery(from, to time.Time, by time.Duration) map[string][]string {
	q := rangeQuery(from.Add(by), to.Add(by))
	q.Set("autoplay", "1")
	return q
}
