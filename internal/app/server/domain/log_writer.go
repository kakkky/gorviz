package domain

import (
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// LogWriter はログ 1 行を書いた goroutine。Found が false なら結べなかった (その時刻に走っていた goroutine が 1 つに決まらない)
type LogWriter struct {
	Log       LogID                   `json:"log"`
	At        time.Time               `json:"at"`
	Goroutine observation.GoroutineID `json:"goroutine,omitempty"`
	Found     bool                    `json:"found"`
	Certainty Certainty               `json:"certainty,omitempty"`
}

// MatchLogWriter はロガーアダプタが振った番号で trace のログの印と結ぶ (FACT)。番号がなければ、その時刻に走っていた goroutine が 1 つだけのときに結ぶ (INFERENCE)
func MatchLogWriter(instance observation.InstanceName, l observation.LogLine, chunks []observation.TraceChunk) LogWriter {
	out := LogWriter{Log: NewLogID(instance, l), At: l.At}
	if l.HasTraceSeq {
		for _, c := range chunks {
			for _, m := range c.LogMarks {
				if m.Seq == l.TraceSeq {
					out.Goroutine, out.Found, out.Certainty = m.Goroutine, true, CertaintyFact
					return out
				}
			}
		}
	}
	for _, c := range chunks {
		if !c.Contains(l.At) {
			continue
		}
		off := c.Offset(l.At)
		running := map[observation.GoroutineID]bool{}
		for _, lane := range c.Procs {
			for _, s := range lane.Spans {
				if s.Goroutine != 0 && s.Start <= off && off <= s.End {
					running[s.Goroutine] = true
				}
			}
		}
		if len(running) == 1 {
			for g := range running {
				out.Goroutine, out.Found, out.Certainty = g, true, CertaintyInference
			}
		}
		return out
	}
	return out
}
