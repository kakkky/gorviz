package domain

import (
	"cmp"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// AroundOccurrence は発生の前後として残す execution trace の幅
const AroundOccurrence = 10 * time.Second

// Occurrence は同じ Issue が 1 つのインスタンスで条件を満たし始めてから満たさなくなるまで。EndedAt が zero の間は続いている
type Occurrence struct {
	Instance    observation.InstanceName `json:"instance"`
	Incarnation observation.Incarnation  `json:"incarnation"`
	Judge       Judge                    `json:"judge"`
	Certainty   Certainty                `json:"certainty"`
	StartedAt   time.Time                `json:"startedAt"`
	LastSeenAt  time.Time                `json:"lastSeenAt"`
	EndedAt     time.Time                `json:"endedAt"`
	Peak        float64                  `json:"peak"`
	Reason      string                   `json:"reason"`
	Impacts     []Impact                 `json:"impacts,omitempty"`
	Sites       []Impact                 `json:"sites,omitempty"`
	TraceFile   string                   `json:"traceFile,omitempty"`
}

func (o Occurrence) Ongoing() bool { return o.EndedAt.IsZero() }

func (o Occurrence) key() string {
	return string(o.Instance) + "\x00" + string(o.Judge) + "\x00" + o.StartedAt.UTC().Format(time.RFC3339Nano)
}

// Around は前後の execution trace として残す期間
func (o Occurrence) Around() (time.Time, time.Time) {
	return o.StartedAt.Add(-AroundOccurrence), o.StartedAt.Add(AroundOccurrence)
}

// Issue は 1 日分の Issue のファイル。同じ Issue は日をまたいで同じ Name のファイルになる
type Issue struct {
	Name        string       `json:"name"`
	Kind        IssueKind    `json:"kind"`
	Subject     string       `json:"subject"`
	Occurrences []Occurrence `json:"occurrences"`
}

// Upsert は同じインスタンス・判定・始まりの発生を置き換える。同じ入力を 2 回処理しても 1 つとみなすため
func (i *Issue) Upsert(o Occurrence) {
	for n, cur := range i.Occurrences {
		if cur.key() == o.key() {
			i.Occurrences[n] = o
			return
		}
	}
	i.Occurrences = append(i.Occurrences, o)
	slices.SortStableFunc(i.Occurrences, func(a, b Occurrence) int {
		return cmp.Or(a.StartedAt.Compare(b.StartedAt), cmp.Compare(a.Instance, b.Instance))
	})
}

// Certainty は発生のうち一番強い確かさ
func (i Issue) Certainty() Certainty {
	var c Certainty
	for _, o := range i.Occurrences {
		c = c.Stronger(o.Certainty)
	}
	return c
}

// IssueDay は発生が始まった日。1 つの Issue は、発生が始まった日ごとのファイルに分かれる
func IssueDay(o Occurrence) time.Time {
	return DataIssues.PeriodStart(o.StartedAt)
}

// TrackedOccurrence はチェックポイントに持つ、まだ追っている発生。TracePending は前後の execution trace をまだ残していない
type TrackedOccurrence struct {
	Issue        string     `json:"issue"`
	Kind         IssueKind  `json:"kind"`
	Subject      string     `json:"subject"`
	Occurrence   Occurrence `json:"occurrence"`
	TracePending bool       `json:"tracePending"`
}

// Tracker は 1 つのインスタンス (普段との比較ならサービス) で続いている発生を追う
type Tracker struct {
	open    []TrackedOccurrence
	changed map[string]TrackedOccurrence
	started []TrackedOccurrence
}

func NewTracker(open []TrackedOccurrence) *Tracker {
	return &Tracker{open: slices.Clone(open), changed: map[string]TrackedOccurrence{}}
}

// Observe は at の時点で judges のやり方で判定した結果を渡す。条件を満たしたものは発生を始めるか続け、
// 同じやり方で続いていて今回満たさなかったものは終える
func (t *Tracker) Observe(at time.Time, instance observation.Instance, judges []Judge, findings []Finding) {
	seen := map[string]bool{}
	for _, f := range findings {
		k := f.IssueName() + "\x00" + string(f.Judge)
		seen[k] = true
		i := slices.IndexFunc(t.open, func(o TrackedOccurrence) bool {
			return o.Issue == f.IssueName() && o.Occurrence.Judge == f.Judge && o.Occurrence.Ongoing()
		})
		if i < 0 {
			o := TrackedOccurrence{Issue: f.IssueName(), Kind: f.Kind, Subject: f.Subject, TracePending: true, Occurrence: Occurrence{
				Instance: instance.Name, Incarnation: instance.Incarnation, Judge: f.Judge, Certainty: f.Certainty,
				StartedAt: at, LastSeenAt: at, Peak: f.Value, Reason: f.Reason, Impacts: f.Impacts, Sites: f.Sites,
			}}
			t.open = append(t.open, o)
			t.started = append(t.started, o)
			t.mark(o)
			continue
		}
		o := &t.open[i]
		o.Occurrence.LastSeenAt = at
		o.Occurrence.Certainty = o.Occurrence.Certainty.Stronger(f.Certainty)
		if f.Value > o.Occurrence.Peak {
			o.Occurrence.Peak, o.Occurrence.Reason, o.Occurrence.Impacts, o.Occurrence.Sites = f.Value, f.Reason, f.Impacts, f.Sites
		}
		t.mark(*o)
	}
	for i := range t.open {
		o := &t.open[i]
		if !o.Occurrence.Ongoing() || !slices.Contains(judges, o.Occurrence.Judge) || seen[o.Issue+"\x00"+string(o.Occurrence.Judge)] {
			continue
		}
		o.Occurrence.EndedAt = at
		t.mark(*o)
	}
}

// End はインスタンスが終わったときに、続いている発生をすべて終える
func (t *Tracker) End(at time.Time) {
	for i := range t.open {
		if o := &t.open[i]; o.Occurrence.Ongoing() {
			o.Occurrence.EndedAt = at
			t.mark(*o)
		}
	}
}

// TraceSaved は前後の execution trace を残した (または残せないと決めた) 発生に印を付ける
func (t *Tracker) TraceSaved(o TrackedOccurrence, file string) {
	for i := range t.open {
		if t.open[i].Occurrence.key() == o.Occurrence.key() && t.open[i].Issue == o.Issue {
			t.open[i].TracePending = false
			t.open[i].Occurrence.TraceFile = file
			t.mark(t.open[i])
		}
	}
}

func (t *Tracker) mark(o TrackedOccurrence) {
	t.changed[o.Issue+"\x00"+o.Occurrence.key()] = o
}

// Open はチェックポイントに残すもの。終わって前後の execution trace も残したものは外す
func (t *Tracker) Open() []TrackedOccurrence {
	var out []TrackedOccurrence
	for _, o := range t.open {
		if o.Occurrence.Ongoing() || o.TracePending {
			out = append(out, o)
		}
	}
	return out
}

func (t *Tracker) Pending() []TrackedOccurrence {
	var out []TrackedOccurrence
	for _, o := range t.open {
		if o.TracePending {
			out = append(out, o)
		}
	}
	return out
}

// Changed は書き直す発生
func (t *Tracker) Changed() []TrackedOccurrence {
	out := make([]TrackedOccurrence, 0, len(t.changed))
	for _, o := range t.changed {
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b TrackedOccurrence) int {
		return cmp.Or(cmp.Compare(a.Issue, b.Issue), a.Occurrence.StartedAt.Compare(b.Occurrence.StartedAt))
	})
	return out
}

func (t *Tracker) Started() []TrackedOccurrence { return t.started }

// ShouldNotify は発生が、どのインスタンスでも続いていない状態から始まったか。others はほかのインスタンス (とサービス) で追っている発生
func ShouldNotify(o TrackedOccurrence, others []TrackedOccurrence) bool {
	for _, x := range others {
		if x.Issue == o.Issue && x.Occurrence.Ongoing() && x.Occurrence.key() != o.Occurrence.key() {
			return false
		}
	}
	return true
}
