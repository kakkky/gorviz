package domain

import (
	"cmp"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// DiagnosisSource は診断を何から算出したか。残っている中で一番細かいものを使う
type DiagnosisSource string

const (
	SourceExecutionTrace  DiagnosisSource = "execution-trace"
	SourceStateBreakdowns DiagnosisSource = "goroutine-state-breakdowns"
	SourceSnapshots       DiagnosisSource = "goroutine-snapshots"
)

// PeriodBy は調べる期間を何で決めたか
type PeriodBy string

const (
	PeriodGiven      PeriodBy = "given"
	PeriodLog        PeriodBy = "log"
	PeriodOccurrence PeriodBy = "issue-occurrence"
	PeriodWoken      PeriodBy = "woken-wait"
)

type Period struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	By   PeriodBy  `json:"by"`
}

func NewPeriod(from, to time.Time, by PeriodBy) (Period, error) {
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return Period{}, ErrInvalidArgument.With("the period needs from before to")
	}
	return Period{From: from, To: to, By: by}, nil
}

// WokenPeriod は、起こされた側 woken が [from, to] で待っていて waker に起こされた区間のうち一番長いもの
func WokenPeriod(joined observation.TraceChunk, woken, waker observation.GoroutineID) (time.Time, time.Time, bool) {
	var best Stretch
	ok := false
	for _, s := range Stretches(joined, woken) {
		if !s.HasLeave || s.Leave.WokenBy != waker || s.Cause == CauseRunning || s.Cause == CauseRunnable {
			continue
		}
		if !ok || s.Duration() > best.Duration() {
			best, ok = s, true
		}
	}
	return joined.StartedAt.Add(best.Start), joined.StartedAt.Add(best.End), ok
}

// Waker は待ちから起こした goroutine と、起こしたときにいた場所
type Waker struct {
	Goroutine observation.GoroutineID `json:"goroutine"`
	Function  string                  `json:"function"`
	Location  string                  `json:"location"`
	Cause     MainCause               `json:"cause"`
	Count     int                     `json:"count"`
	Waited    time.Duration           `json:"waited"`
	From      time.Time               `json:"from"`
	To        time.Time               `json:"to"`
}

// LeakStatus は goroutine ごとのリーク。Checked が false ならリーク判定が残っていないか、runtime が判定できない
type LeakStatus struct {
	Checked   bool                        `json:"checked"`
	CheckedAt time.Time                   `json:"checkedAt"`
	Status    observation.LeakCheckStatus `json:"status"`
	Leaked    bool                        `json:"leaked"`
	Certainty Certainty                   `json:"certainty,omitempty"`
	Count     int                         `json:"count,omitempty"`
	Of        int                         `json:"of,omitempty"`
}

// Diagnosis は 1 つの goroutine の、調べる期間についての診断。Breakdown が nil なら状態別の時間内訳は出せない (スナップショットからの算出)
type Diagnosis struct {
	Source        DiagnosisSource
	ComputedFrom  Period
	MinuteAligned bool
	Entry         string
	CreatedBy     observation.Frame
	Breakdown     *StateBreakdown
	Main          MainCause
	MainCertainty Certainty
	Stop          *StopPlace
	Wakers        []Waker
}

// DiagnoseTrace は [from, to] をつないだチャンクから診断する。g が期間に現れなければ false
func DiagnoseTrace(joined observation.TraceChunk, g observation.GoroutineID, from, to time.Time) (Diagnosis, bool) {
	lo, hi := joined.Offset(from), joined.Offset(to)
	b := Breakdown(joined, g, lo, hi)
	main, ok := b.Main()
	if !ok {
		return Diagnosis{}, false
	}
	d := Diagnosis{Source: SourceExecutionTrace, ComputedFrom: Period{From: from, To: to}, Entry: joined.Entries[g], Breakdown: &b, Main: main, MainCertainty: CertaintyFact}
	if main != CauseRunning {
		if s, ok := LongestStop(joined, g, main, lo, hi); ok {
			d.Stop = &s
		}
	} else if s, ok := longestNonRunning(joined, g, b, lo, hi); ok {
		d.Stop = &s
	}
	wakers := map[observation.GoroutineID]*Waker{}
	for _, s := range Stretches(joined, g) {
		if !s.HasLeave || s.Leave.WokenBy == 0 || s.End < lo || s.Start > hi {
			continue
		}
		w, ok := wakers[s.Leave.WokenBy]
		if !ok {
			w = &Waker{Goroutine: s.Leave.WokenBy, Function: s.Leave.WokenByFunction, Location: s.Leave.WokenByLocation, Cause: s.Cause,
				From: joined.StartedAt.Add(s.Start), To: joined.StartedAt.Add(s.End)}
			wakers[s.Leave.WokenBy] = w
		}
		w.Count++
		w.Waited += s.Duration()
	}
	for _, w := range wakers {
		d.Wakers = append(d.Wakers, *w)
	}
	slices.SortFunc(d.Wakers, func(a, b Waker) int {
		return cmp.Or(cmp.Compare(b.Waited, a.Waited), cmp.Compare(a.Goroutine, b.Goroutine))
	})
	return d, true
}

func longestNonRunning(c observation.TraceChunk, g observation.GoroutineID, b StateBreakdown, lo, hi time.Duration) (StopPlace, bool) {
	var best time.Duration
	var cause MainCause
	for _, k := range MainCauses {
		if k != CauseRunning && b.Durations[k] > best {
			best, cause = b.Durations[k], k
		}
	}
	if best == 0 {
		return StopPlace{}, false
	}
	return LongestStop(c, g, cause, lo, hi)
}

// DiagnoseBreakdowns は 1 分ごとの内訳から診断する。期間は 1 分単位に広げて合わせる
func DiagnoseBreakdowns(rows []MinuteBreakdown, g observation.GoroutineID, from, to time.Time) (Diagnosis, bool) {
	aFrom, aTo := from.UTC().Truncate(BreakdownStep), to.UTC().Truncate(BreakdownStep)
	if aTo.Before(to) {
		aTo = aTo.Add(BreakdownStep)
	}
	var (
		total StateBreakdown
		entry string
		stops = map[MainCause]StopPlace{}
		found bool
	)
	for _, r := range rows {
		if r.Goroutine != g || r.Minute.Before(aFrom) || !r.Minute.Before(aTo) {
			continue
		}
		found = true
		total.Add(r.Breakdown)
		entry = cmp.Or(entry, r.Entry)
		for c, s := range r.Stops {
			if s.Duration > stops[c].Duration {
				stops[c] = s
			}
		}
	}
	if !found {
		return Diagnosis{}, false
	}
	main, ok := total.Main()
	if !ok {
		return Diagnosis{}, false
	}
	d := Diagnosis{Source: SourceStateBreakdowns, ComputedFrom: Period{From: aFrom, To: aTo}, MinuteAligned: !aFrom.Equal(from) || !aTo.Equal(to),
		Entry: entry, Breakdown: &total, Main: main, MainCertainty: CertaintyFact}
	stopCause := main
	if main == CauseRunning {
		var best time.Duration
		for _, k := range MainCauses {
			if k != CauseRunning && total.Durations[k] > best {
				best, stopCause = total.Durations[k], k
			}
		}
	}
	if s, ok := stops[stopCause]; ok {
		d.Stop = &s
	}
	return d, true
}

// DiagnoseSnapshots は期間の中で一番新しいスナップショット (なければ一番近いもの) の、その瞬間の状態から診断する
func DiagnoseSnapshots(snapshots []observation.GoroutineSnapshot, g observation.GoroutineID, from, to time.Time) (Diagnosis, bool) {
	var (
		snap observation.GoroutineSnapshot
		gr   observation.Goroutine
		ok   bool
	)
	for _, s := range snapshots {
		i := slices.IndexFunc(s.Goroutines, func(x observation.Goroutine) bool { return x.ID == g })
		if i < 0 {
			continue
		}
		inPeriod := !s.At.Before(from) && !s.At.After(to)
		if !ok || inPeriod || s.At.Sub(to).Abs() < snap.At.Sub(to).Abs() {
			snap, gr, ok = s, s.Goroutines[i], true
		}
	}
	if !ok {
		return Diagnosis{}, false
	}
	main := CauseOfSnapshot(gr)
	cert := CertaintyFact
	if gr.WaitReasonInferred {
		cert = CertaintyInference
	}
	return Diagnosis{
		Source: SourceSnapshots, ComputedFrom: Period{From: snap.At, To: snap.At}, Entry: gr.Entry, CreatedBy: gr.CreatedBy,
		Main: main, MainCertainty: cert,
		Stop: &StopPlace{Cause: main, Function: gr.Current.Function, Location: gr.Current.Location(), WaitReason: gr.WaitReason, Duration: gr.Waiting},
	}, true
}

// DiagnoseLeak は to より前で一番新しいリーク判定を、一番近いスナップショットと突き合わせる
func DiagnoseLeak(checks []observation.GoroutineLeakCheck, snapshots []observation.GoroutineSnapshot, g observation.GoroutineID, to time.Time) LeakStatus {
	var (
		lc observation.GoroutineLeakCheck
		ok bool
	)
	for _, c := range checks {
		if !c.At.After(to) && (!ok || c.At.After(lc.At)) {
			lc, ok = c, true
		}
	}
	if !ok {
		return LeakStatus{}
	}
	st := LeakStatus{Checked: lc.Status == observation.LeakCheckDone, CheckedAt: lc.At, Status: lc.Status}
	snap, has := NearestSnapshot(snapshots, lc.At)
	if !st.Checked || !has {
		return st
	}
	if m, leaked := LeakOf(MatchLeaks(lc, snap), g); leaked {
		st.Leaked, st.Certainty, st.Count, st.Of = true, m.Certainty, m.Count, m.Of
	}
	return st
}

// GoroutineStateChange は 1 つの goroutine の状態の変化 1 つを、壁時計の時刻で
type GoroutineStateChange struct {
	At              time.Time               `json:"at"`
	Place           observation.PlaceKind   `json:"place"`
	Cause           MainCause               `json:"cause,omitempty"`
	Reason          string                  `json:"reason,omitempty"`
	Function        string                  `json:"function,omitempty"`
	Location        string                  `json:"location,omitempty"`
	Proc            observation.ProcID      `json:"proc"`
	HasProc         bool                    `json:"hasProc"`
	Thread          observation.ThreadID    `json:"thread"`
	Created         bool                    `json:"created,omitempty"`
	WokenBy         observation.GoroutineID `json:"wokenBy,omitempty"`
	WokenByFunction string                  `json:"wokenByFunction,omitempty"`
	WokenByLocation string                  `json:"wokenByLocation,omitempty"`
}

func StateChangesOf(joined observation.TraceChunk, g observation.GoroutineID) []GoroutineStateChange {
	var out []GoroutineStateChange
	for _, ev := range joined.Events {
		if ev.Kind != observation.StateChangeGoroutine || ev.Goroutine != g {
			continue
		}
		cause, _ := CauseOfPlace(ev.Place, ev.Function)
		out = append(out, GoroutineStateChange{
			At: joined.StartedAt.Add(ev.At), Place: ev.Place.Kind, Cause: cause, Reason: ev.Place.Reason, Function: ev.Function, Location: ev.Location,
			Proc: ev.Place.Proc, HasProc: ev.Place.HasProc, Thread: ev.Place.Thread, Created: ev.Created,
			WokenBy: ev.WokenBy, WokenByFunction: ev.WokenByFunction, WokenByLocation: ev.WokenByLocation,
		})
	}
	return out
}
