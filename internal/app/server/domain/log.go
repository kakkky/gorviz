package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// StoredLog はサービスのログ 1 行と、書いたインスタンス
type StoredLog struct {
	Instance    observation.InstanceName `json:"instance"`
	Incarnation observation.Incarnation  `json:"incarnation"`
	observation.LogLine
}

// LogID はサービスの中でログ 1 行を指す。<時刻>~<インスタンス>~<agent>~<番号>
type LogID string

const logIDTime = "20060102T150405.000000000Z"

func NewLogID(instance observation.InstanceName, l observation.LogLine) LogID {
	return LogID(fmt.Sprintf("%s~%s~%s~%d", l.At.UTC().Format(logIDTime), instance, l.Agent, l.Seq))
}

func (l StoredLog) ID() LogID { return NewLogID(l.Instance, l.LogLine) }

type ParsedLogID struct {
	At       time.Time
	Instance observation.InstanceName
	Agent    observation.AgentID
	Seq      uint64
}

func ParseLogID(id LogID) (ParsedLogID, error) {
	s := string(id)
	at, rest, ok := strings.Cut(s, "~")
	i := strings.LastIndex(rest, "~")
	if !ok || i < 0 {
		return ParsedLogID{}, ErrInvalidArgument.With("the log id is not <time>~<instance>~<agent>~<seq>: " + s)
	}
	seq, err := strconv.ParseUint(rest[i+1:], 10, 64)
	rest = rest[:i]
	j := strings.LastIndex(rest, "~")
	t, terr := time.Parse(logIDTime, at)
	if err != nil || j < 0 || terr != nil {
		return ParsedLogID{}, ErrInvalidArgument.With("the log id is not <time>~<instance>~<agent>~<seq>: " + s)
	}
	return ParsedLogID{At: t, Instance: observation.InstanceName(rest[:j]), Agent: observation.AgentID(rest[j+1:]), Seq: seq}, nil
}

// Attrs は JSON のログ行のキーと値。入れ子は . でつないだキーにする。JSON でなければ空
func (l StoredLog) Attrs() map[string]string {
	var m map[string]any
	if err := json.Unmarshal([]byte(l.Raw), &m); err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	flatten("", m, out)
	return out
}

func flatten(prefix string, m map[string]any, out map[string]string) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch t := v.(type) {
		case map[string]any:
			flatten(key, t, out)
		case string:
			out[key] = t
		default:
			b, _ := json.Marshal(t)
			out[key] = string(b)
		}
	}
}

// LogFilter はログを絞る条件
type LogFilter struct {
	Level    observation.LogLevel
	Instance observation.InstanceName
	Attrs    map[string]string
	Text     string
}

func (f LogFilter) Match(l StoredLog) bool {
	if f.Level != "" && !l.Level.AtLeast(f.Level) {
		return false
	}
	if f.Instance != "" && l.Instance != f.Instance {
		return false
	}
	if f.Text != "" && !strings.Contains(strings.ToLower(l.Raw+" "+l.Message), strings.ToLower(f.Text)) {
		return false
	}
	if len(f.Attrs) > 0 {
		attrs := l.Attrs()
		for k, v := range f.Attrs {
			if attrs[k] != v {
				return false
			}
		}
	}
	return true
}

// LogKey はログの集計で数えるキーの値。level・instance・message はログの項目、それ以外は JSON のキー
func LogKey(l StoredLog, by string) (string, bool) {
	switch by {
	case "level":
		return string(l.Level), true
	case "instance":
		return string(l.Instance), true
	case "message":
		return l.Message, true
	case "message-shape":
		return MessageShape(l.Message), true
	}
	v, ok := l.Attrs()[by]
	return v, ok
}

// LogPeriod はログから goroutine を開いたときに調べる期間。所要時間があればそのリクエストの区間、なければ書いた時刻の前後
const AroundLog = 5 * time.Second

func LogPeriod(l observation.LogLine) (time.Time, time.Time) {
	if l.HasDuration && l.Duration > 0 {
		return l.RequestStartedAt(), l.At
	}
	return l.At.Add(-AroundLog), l.At.Add(AroundLog)
}

// AroundErrorLog はエラーログの前後として残す execution trace の期間。所要時間があればリクエストの区間を含める
func AroundErrorLog(l observation.LogLine) (time.Time, time.Time) {
	from := l.At.Add(-AroundOccurrence)
	if l.HasDuration && l.RequestStartedAt().Before(from) {
		from = maxTime(l.RequestStartedAt(), l.At.Add(-time.Minute))
	}
	return from, l.At.Add(AroundLog)
}

// SummarySize は一覧に出すときの大きさの見込み (バイト)。件数ではなく返す量で区切るため
func (l StoredLog) SummarySize() int {
	return len(l.Message) + len(l.Instance) + len(l.Agent) + 120
}
