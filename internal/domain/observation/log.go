package observation

import "time"

type LogLevel string

const (
	LogLevelUnknown LogLevel = ""
	LogLevelDebug   LogLevel = "DEBUG"
	LogLevelInfo    LogLevel = "INFO"
	LogLevelWarn    LogLevel = "WARN"
	LogLevelError   LogLevel = "ERROR"
)

func (l LogLevel) rank() int {
	switch l {
	case LogLevelDebug:
		return 1
	case LogLevelInfo:
		return 2
	case LogLevelWarn:
		return 3
	case LogLevelError:
		return 4
	}
	return 0
}

func (l LogLevel) AtLeast(o LogLevel) bool {
	return l.rank() >= o.rank()
}

type LogSource string

const (
	LogSourceFile   LogSource = "file"
	LogSourceTCP    LogSource = "tcp"
	LogSourceUDP    LogSource = "udp"
	LogSourceStdin  LogSource = "stdin"
	LogSourceLogger LogSource = "logger"
)

// LogLine の At は、ログに時刻がなければ取り込んだ時刻 (TimeFromLog が false)。
// TraceSeq はロガーアダプタが振った番号で、TraceChunk の LogMark と同じ番号
type LogLine struct {
	Agent       AgentID
	Seq         uint64
	At          time.Time
	TimeFromLog bool
	Level       LogLevel
	Message     string
	Raw         string
	Source      LogSource
	TraceSeq    uint64
	HasTraceSeq bool
	Duration    time.Duration
	HasDuration bool
}

// RequestStartedAt は所要時間のあるログの、リクエストが始まった時刻。ログはリクエストが終わってから書かれる
func (l LogLine) RequestStartedAt() time.Time {
	return l.At.Add(-l.Duration)
}
