package web

import (
	"encoding/json"
	"slices"
	"strconv"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web/view/components"
)

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

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// replayWindow は再生する窓の JSON。関数名と待機理由を表に詰め、イベントはその番号で引く
func replayWindow(w domain.SchedulerWindow, leaks []domain.LeakStack) string {
	var fns, reasons table
	out := components.ReplayWindow{From: at(w.From), To: at(w.To), StartedAtMs: float64(w.From) / 1e6, DurationNs: int64(w.To - w.From), Entries: map[string]int{}, Truncated: w.Truncated, Ended: w.Ended}
	for _, c := range w.Changes {
		e := components.ReplayEvent{At: int64(c.At), G: int64(c.G), Place: int(c.Place), Proc: int64(c.Proc), Thread: int64(c.Thread), Reason: reasons.of(w.Reasons[c.Reason]), Function: fns.of(w.Functions[c.Stack]), Cause: int(c.Cause) - 1, WokenBy: int64(c.WokenBy)}
		if c.Place != domain.PlaceWaiting {
			e.Reason = -1
		}
		out.Events = append(out.Events, e)
	}
	for _, p := range w.ProcChanges {
		out.ProcEvents = append(out.ProcEvents, components.ReplayProc{At: int64(p.At), Proc: int64(p.Proc), Running: p.Running, Thread: int64(p.Thread)})
	}
	for _, p := range w.Procs {
		out.Procs = append(out.Procs, int64(p))
		var lane []components.ReplaySpan
		for _, s := range w.Lanes[p] {
			lane = append(lane, components.ReplaySpan{Start: int64(s.Start), End: int64(s.End), G: int64(s.G)})
		}
		out.Lanes = append(out.Lanes, orEmpty(lane))
	}
	for _, r := range w.STW {
		out.STW = append(out.STW, components.ReplayRange{Start: int64(r.Start), End: int64(r.End), Reason: reasons.of(r.Reason), ByGC: len(r.Reason) > 3 && r.Reason[:3] == "GC "})
	}
	for _, r := range w.GCMark {
		out.GCMark = append(out.GCMark, components.ReplayRange{Start: int64(r.Start), End: int64(r.End), Reason: -1, ByGC: true})
	}
	for g, e := range w.Entries {
		out.Entries[strconv.FormatInt(int64(g), 10)] = fns.of(e)
	}
	for g := range w.Agents {
		out.Agents = append(out.Agents, int64(g))
	}
	slices.Sort(out.Agents)
	for _, l := range leaks {
		for _, g := range l.Goroutines {
			out.Leaks = append(out.Leaks, components.ReplayLeak{G: int64(g), Certainty: l.Certainty, Count: l.Count, Of: l.Of})
		}
	}
	out.Functions, out.Reasons = orEmpty(fns.names), orEmpty(reasons.names)
	out.Events, out.ProcEvents, out.STW, out.GCMark, out.Leaks, out.Agents, out.Lanes = orEmpty(out.Events), orEmpty(out.ProcEvents), orEmpty(out.STW), orEmpty(out.GCMark), orEmpty(out.Leaks), orEmpty(out.Agents), orEmpty(out.Lanes)
	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func propsJSON(p components.GMPReplayProps) string {
	b, err := json.Marshal(p)
	if err != nil {
		return "{}"
	}
	return string(b)
}
