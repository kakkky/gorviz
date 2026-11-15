package domain

import "time"

// TraceChunk は録画の 1 区切り。trace は同時に 1 本しか取れないので、録画ループが区切りを間を空けずに取り続け、
// Live も Explore もここから読む。
type TraceChunk struct {
	// StartedAt は Timeline の時刻 0 に当たる壁時計の時刻。trace の時計とログの時刻を対応づけるのに使う
	StartedAt time.Time
	Timeline  SchedulerTimeline
	LogMarks  []TraceLogMark
}

// TraceLogMark は対象が gorviz の slog ハンドラ経由でログを書いた瞬間の記録。どの G がいつ書いたかが正確に分かる
type TraceLogMark struct {
	Seq       uint64
	Goroutine GoroutineID
	At        time.Duration
}

func (c TraceChunk) EndedAt() time.Time {
	return c.StartedAt.Add(c.Timeline.Window)
}

func (c TraceChunk) Contains(t time.Time) bool {
	return !t.Before(c.StartedAt) && !t.After(c.EndedAt())
}

// Offset は壁時計の時刻を Timeline 上の時刻に直す。
func (c TraceChunk) Offset(t time.Time) time.Duration {
	return t.Sub(c.StartedAt)
}

type TraceRecording interface {
	Append(chunk TraceChunk)
	Latest() (TraceChunk, bool)
	At(t time.Time) (TraceChunk, bool)
	FindLogMark(seq uint64) (TraceChunk, TraceLogMark, bool)
}
