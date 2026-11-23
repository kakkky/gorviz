package domain

import (
	"cmp"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

const sampleGoroutines = 10

// CreatorSummary は生成元の関数ごとにまとめた goroutine。Breakdown は状態別の時間の合計
type CreatorSummary struct {
	Creator    string                    `json:"creator"`
	Goroutines int                       `json:"goroutines"`
	Breakdown  StateBreakdown            `json:"breakdown"`
	Main       MainCause                 `json:"main"`
	Sample     []observation.GoroutineID `json:"sample"`
}

// Creators は goroutine ごとの生成元。スナップショットにあれば生成元、なければ入口で代用する
func Creators(snapshots []observation.GoroutineSnapshot) map[observation.GoroutineID]string {
	out := map[observation.GoroutineID]string{}
	for _, s := range snapshots {
		for _, g := range s.Goroutines {
			out[g.ID] = creatorOf(g)
		}
	}
	return out
}

type summaryAcc struct {
	out  map[string]*CreatorSummary
	seen map[string]map[observation.GoroutineID]bool
}

func newSummaryAcc() *summaryAcc {
	return &summaryAcc{out: map[string]*CreatorSummary{}, seen: map[string]map[observation.GoroutineID]bool{}}
}

func (a *summaryAcc) add(creator string, g observation.GoroutineID, b StateBreakdown) {
	s, ok := a.out[creator]
	if !ok {
		s = &CreatorSummary{Creator: creator, Breakdown: StateBreakdown{Durations: map[MainCause]time.Duration{}}}
		a.out[creator], a.seen[creator] = s, map[observation.GoroutineID]bool{}
	}
	if !a.seen[creator][g] {
		a.seen[creator][g] = true
		s.Goroutines++
		if len(s.Sample) < sampleGoroutines {
			s.Sample = append(s.Sample, g)
		}
	}
	s.Breakdown.Add(b)
}

func (a *summaryAcc) result() []CreatorSummary {
	out := make([]CreatorSummary, 0, len(a.out))
	for _, s := range a.out {
		s.Main, _ = s.Breakdown.Main()
		slices.Sort(s.Sample)
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b CreatorSummary) int {
		return cmp.Or(cmp.Compare(b.Breakdown.Total(), a.Breakdown.Total()), cmp.Compare(b.Goroutines, a.Goroutines), cmp.Compare(a.Creator, b.Creator))
	})
	return out
}

func creatorOr(creators map[observation.GoroutineID]string, g observation.GoroutineID, entry string) string {
	if c, ok := creators[g]; ok {
		return c
	}
	return NormalizeFunction(entry)
}

// SummarizeTrace はつないだチャンクの [from, to] の goroutine を生成元ごとにまとめる
func SummarizeTrace(joined observation.TraceChunk, creators map[observation.GoroutineID]string, from, to time.Time) []CreatorSummary {
	acc := newSummaryAcc()
	lo, hi := joined.Offset(from), joined.Offset(to)
	for _, g := range Goroutines(joined, 0, joined.Duration) {
		b := Breakdown(joined, g, lo, hi)
		if b.Total() == 0 {
			continue
		}
		b.Unrecorded = 0
		acc.add(creatorOr(creators, g, joined.Entries[g]), g, b)
	}
	return acc.result()
}

func SummarizeBreakdowns(rows []MinuteBreakdown, creators map[observation.GoroutineID]string) []CreatorSummary {
	acc := newSummaryAcc()
	for _, r := range rows {
		b := r.Breakdown
		b.Unrecorded = 0
		acc.add(creatorOr(creators, r.Goroutine, r.Entry), r.Goroutine, b)
	}
	return acc.result()
}

// SummarizeSnapshot は 1 つのスナップショットの goroutine を生成元ごとに数える。時間は持たないので、状態ごとの数を主因に使う
func SummarizeSnapshot(s observation.GoroutineSnapshot) []CreatorSummary {
	acc := newSummaryAcc()
	for _, g := range s.Goroutines {
		acc.add(creatorOf(g), g.ID, StateBreakdown{Durations: map[MainCause]time.Duration{CauseOfSnapshot(g): max(g.Waiting, 1)}})
	}
	return acc.result()
}

// SnapshotGroup はある時刻の goroutine を、生成元と止まっている場所ごとにまとめたもの。Leaked はそのうちリークと決めた数
type SnapshotGroup struct {
	Creator         string                     `json:"creator"`
	CreatorLocation string                     `json:"creatorLocation"`
	Function        string                     `json:"function"`
	Location        string                     `json:"location"`
	State           observation.GoroutineState `json:"state"`
	Cause           MainCause                  `json:"cause"`
	WaitReason      string                     `json:"waitReason,omitempty"`
	Count           int                        `json:"count"`
	MaxWaiting      time.Duration              `json:"maxWaiting"`
	Goroutines      []observation.GoroutineID  `json:"goroutines"`
	Leaked          int                        `json:"leaked,omitempty"`
	LeakCertainty   Certainty                  `json:"leakCertainty,omitempty"`
}

func GroupSnapshot(s observation.GoroutineSnapshot, leaks []LeakMatch) []SnapshotGroup {
	groups := map[string]*SnapshotGroup{}
	for _, g := range s.Goroutines {
		k := creatorOf(g) + "\x00" + g.Current.Location() + "\x00" + string(g.State) + "\x00" + g.WaitReason
		gr, ok := groups[k]
		if !ok {
			gr = &SnapshotGroup{Creator: creatorOf(g), CreatorLocation: g.CreatedBy.Location(), Function: g.Current.Function, Location: g.Current.Location(),
				State: g.State, Cause: CauseOfSnapshot(g), WaitReason: g.WaitReason}
			groups[k] = gr
		}
		gr.Count++
		gr.MaxWaiting = max(gr.MaxWaiting, g.Waiting)
		if len(gr.Goroutines) < sampleGoroutines {
			gr.Goroutines = append(gr.Goroutines, g.ID)
		}
		if m, leaked := LeakOf(leaks, g.ID); leaked {
			gr.Leaked++
			gr.LeakCertainty = gr.LeakCertainty.Stronger(m.Certainty)
		}
	}
	out := make([]SnapshotGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	slices.SortFunc(out, func(a, b SnapshotGroup) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Creator, b.Creator), cmp.Compare(a.Location, b.Location))
	})
	return out
}
