package domain

import "time"

// TraceChunk は録画の 1 区切り。trace は同時に 1 本しか取れないので、録画ループが区切りを間を空けずに取り続け、
// Live も Explore もここから読む。
type TraceChunk struct {
	// StartedAt は Timeline の時刻 0 に当たる壁時計の時刻。trace の時計とログの時刻を対応づけるのに使う
	StartedAt time.Time
	Timeline  SchedulerTimeline
	LogMarks  []TraceLogMark
	// Raw は取ったままの trace。`go tool trace` で開けるように書き出したり、保存したりするのに使う
	Raw []byte
}

// schedEventBytes は読み込んだ再生用イベント 1 つがメモリで占める大きさの目安
const schedEventBytes = 96

// SizeBytes はリングバッファの上限に数える大きさ。生の trace と、読み込んだイベントの両方を数える
func (c TraceChunk) SizeBytes() int64 {
	return int64(len(c.Raw)) + int64(len(c.Timeline.Events))*schedEventBytes
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
	// Between は [from, to] に重なる区切りを古い順に返す
	Between(from, to time.Time) []TraceChunk
	FindLogMark(seq uint64) (TraceChunk, TraceLogMark, bool)
	// Span は今持てている区間と、その大きさ。持てる時間は trace の量 (対象の忙しさ) で変わる
	Span() (RecordingSpan, bool)
}

type RecordingSpan struct {
	From       time.Time
	To         time.Time
	Bytes      int64
	LimitBytes int64
}

// SavedRecording は server に保存した、Issue の前後の録画 1 件。agent のリングバッファから消えたあとも、これで調べられる
type SavedRecording struct {
	ID      string
	Kind    IssueKind
	Subject string
	At      time.Time
	From    time.Time
	To      time.Time
	Chunks  int
	Bytes   int64
}

type SavedRecordings interface {
	// Save は録画を保存する。同じ ID がすでにあるかは Has で確かめる
	Save(rec SavedRecording, chunks []TraceChunk) error
	Has(id string) bool
	List() []SavedRecording
	Chunks(id string) ([]TraceChunk, bool)
}
