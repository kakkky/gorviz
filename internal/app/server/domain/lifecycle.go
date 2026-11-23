package domain

import (
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

type LifecycleKind string

const (
	LifecycleStart LifecycleKind = "START"
	LifecycleEnd   LifecycleKind = "END"
)

// LifecycleEvent の Restart は、同じ名前の前のインスタンスがあった (再起動・入れ替わり) 始まり
type LifecycleEvent struct {
	At          time.Time                `json:"at"`
	Instance    observation.InstanceName `json:"instance"`
	Incarnation observation.Incarnation  `json:"incarnation"`
	Kind        LifecycleKind            `json:"kind"`
	Restart     bool                     `json:"restart,omitempty"`
}

// LifecycleChanges は前に記録した last と届いた Batch から、記録する始まりと終わりを決める。
// Incarnation が変わったら、前の終わりと新しい始まりを記録する
func LifecycleChanges(last LifecycleEvent, hasLast bool, b observation.Batch, now time.Time) []LifecycleEvent {
	startAt, ok := Earliest(b)
	if !ok {
		startAt = now
	}
	endAt, ok := Latest(b)
	if !ok {
		endAt = now
	}
	var out []LifecycleEvent
	current := hasLast && last.Incarnation == b.Instance.Incarnation
	switch {
	case current && last.Kind == LifecycleEnd:
		return nil
	case !current:
		if hasLast && last.Kind == LifecycleStart {
			out = append(out, LifecycleEvent{At: minTime(startAt, now), Instance: last.Instance, Incarnation: last.Incarnation, Kind: LifecycleEnd})
		}
		out = append(out, LifecycleEvent{At: startAt, Instance: b.Instance.Name, Incarnation: b.Instance.Incarnation, Kind: LifecycleStart, Restart: hasLast})
	}
	if b.Ended {
		out = append(out, LifecycleEvent{At: endAt, Instance: b.Instance.Name, Incarnation: b.Instance.Incarnation, Kind: LifecycleEnd})
	}
	return out
}

// Incarnation は 1 つのインスタンス (同じ名前のプロセスの 1 回分) の始まりと終わり
type Incarnation struct {
	Incarnation observation.Incarnation `json:"incarnation"`
	StartedAt   time.Time               `json:"startedAt"`
	EndedAt     time.Time               `json:"endedAt"`
	Restart     bool                    `json:"restart"`
}

func (i Incarnation) Ended() bool { return !i.EndedAt.IsZero() }

// Incarnations は時刻順の記録を、インスタンスの名前ごとに 1 回分ずつにまとめる
func Incarnations(events []LifecycleEvent) map[observation.InstanceName][]Incarnation {
	out := map[observation.InstanceName][]Incarnation{}
	for _, e := range events {
		list := out[e.Instance]
		switch e.Kind {
		case LifecycleStart:
			list = append(list, Incarnation{Incarnation: e.Incarnation, StartedAt: e.At, Restart: e.Restart})
		case LifecycleEnd:
			if n := len(list); n > 0 && list[n-1].Incarnation == e.Incarnation && !list[n-1].Ended() {
				list[n-1].EndedAt = e.At
			} else {
				list = append(list, Incarnation{Incarnation: e.Incarnation, EndedAt: e.At})
			}
		}
		out[e.Instance] = list
	}
	return out
}
