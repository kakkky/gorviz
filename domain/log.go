package domain

import "time"

type LogLevel string

const (
	LogLevelUnknown LogLevel = ""
	LogLevelDebug   LogLevel = "debug"
	LogLevelInfo    LogLevel = "info"
	LogLevelWarn    LogLevel = "warn"
	LogLevelError   LogLevel = "error"
)

type LogLineID int64

// LogLine は対象が出したログの 1 行。
// At はログに書かれた時刻 (読めなければ受け取った時刻で、TimeFromLog が false)。
// TraceSeq は gorviz の slog ハンドラが付けた番号で、trace の TraceLogMark と突き合わせて書いた G を特定する
type LogLine struct {
	ID          LogLineID
	At          time.Time
	TimeFromLog bool
	Level       LogLevel
	Message     string
	Raw         string
	Source      string
	TraceSeq    uint64
	HasTraceSeq bool
}

type LogRecording interface {
	Append(line LogLine) LogLine
	Snapshot() []LogLine
	Get(id LogLineID) (LogLine, bool)
}

type LogParser interface {
	Parse(raw string, source string, receivedAt time.Time) LogLine
}
