package model

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
// TraceSeq は gorviz のロガーの差し込み口が付けた番号で、trace の TraceLogMark と突き合わせて書いた G を特定する。
// Duration はログに載った所要時間 (アクセスログの duration など)。リクエストが終わってから書かれるので、書いた時刻の Duration 前から書いた時刻までがそのリクエストの区間
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
	Duration    time.Duration
	HasDuration bool
}

// RequestStartedAt は所要時間のあるログの、リクエストが始まった時刻
func (l LogLine) RequestStartedAt() time.Time {
	return l.At.Add(-l.Duration)
}
