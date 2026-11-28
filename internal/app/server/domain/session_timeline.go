package domain

import (
	"strconv"
	"strings"
	"time"
)

// trace の上に置く出来事の名前
const (
	MarkDiagnoseBegin = "diagnose begin"
	MarkDiagnoseEnd   = "diagnose end"
	MarkShutdown      = "shutdown"
	markPhasePrefix   = "phase "
)

const (
	PhaseSmoke        = "smoke"
	PhaseCancellation = "cancellation"
	PhaseCooldown     = "cooldown"
	PhaseShutdown     = "shutdown"
)

func RampUpPhase(level int) string {
	return "ramp-up " + strconv.Itoa(level)
}

func PhaseMark(phase string) string {
	return markPhasePrefix + phase
}

type SessionEventKind string

const (
	EventAppStart      SessionEventKind = "app-start"
	EventDiagnoseBegin SessionEventKind = "diagnose-begin"
	EventDiagnoseEnd   SessionEventKind = "diagnose-end"
	EventPhase         SessionEventKind = "phase"
	EventShutdown      SessionEventKind = "shutdown"
	EventAppExit       SessionEventKind = "app-exit"
)

// SessionEvent はセッションの時間軸に出す出来事
type SessionEvent struct {
	At    time.Duration
	Kind  SessionEventKind
	Phase string
	Note  string
}

// PhaseRange は phase が流れていた期間。次の出来事までを 1 つの phase とする
type PhaseRange struct {
	Name string
	From time.Duration
	To   time.Duration
}

func PhasesOf(marks []TraceMark, last time.Duration) []PhaseRange {
	var out []PhaseRange
	for i, m := range marks {
		if !strings.HasPrefix(m.Message, markPhasePrefix) {
			continue
		}
		to := last
		if i+1 < len(marks) {
			to = marks[i+1].At
		}
		out = append(out, PhaseRange{Name: strings.TrimPrefix(m.Message, markPhasePrefix), From: m.At, To: to})
	}
	// trace の末尾は欠けるので、すぐ終わるアプリでは止める合図が trace の最後より後になる。それでも shutdown を空にしない
	if at, ok := markAt(marks, MarkShutdown); ok {
		out = append(out, PhaseRange{Name: PhaseShutdown, From: min(at, last), To: last + 1})
	}
	return out
}

func markAt(marks []TraceMark, message string) (time.Duration, bool) {
	for _, m := range marks {
		if m.Message == message {
			return m.At, true
		}
	}
	return 0, false
}

// PhaseAt は at を含む phase。phase の外なら空
func PhaseAt(phases []PhaseRange, at time.Duration) string {
	for _, p := range phases {
		if p.From <= at && at < p.To {
			return p.Name
		}
	}
	return ""
}

// TimelineOf は trace の出来事とアプリの終わりから、セッションの時間軸を組む
func TimelineOf(marks []TraceMark, s Session) []SessionEvent {
	out := []SessionEvent{{At: 0, Kind: EventAppStart}}
	for _, m := range marks {
		switch {
		case m.Message == MarkDiagnoseBegin:
			out = append(out, SessionEvent{At: m.At, Kind: EventDiagnoseBegin})
		case m.Message == MarkDiagnoseEnd:
			out = append(out, SessionEvent{At: m.At, Kind: EventDiagnoseEnd})
		case m.Message == MarkShutdown:
			out = append(out, SessionEvent{At: m.At, Kind: EventShutdown})
		case strings.HasPrefix(m.Message, markPhasePrefix):
			out = append(out, SessionEvent{At: m.At, Kind: EventPhase, Phase: strings.TrimPrefix(m.Message, markPhasePrefix)})
		}
	}
	if s.App.Exited {
		note := "exit code " + strconv.Itoa(s.App.ExitCode)
		if s.App.TraceTruncated {
			note += ", the end of the trace is missing"
		}
		if s.App.ShutdownTimedOut {
			note += ", shutdown did not finish in time"
		}
		out = append(out, SessionEvent{At: s.App.ExitedAt, Kind: EventAppExit, Note: note})
	}
	return out
}
