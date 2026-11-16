package domain

import (
	"github.com/kakkky/gorviz/internal/app/api/model"
	"time"
)

// DumpSource は直近の goroutine dump の取得元。server は agent が取った dump をこれで読む (自分では対象に触らない)
type DumpSource interface {
	LatestDump() (model.GoroutineDump, bool)
}

// LogSource は対象のログの取得元。server は agent が取り込んだログを after より新しい分だけ読む
type LogSource interface {
	LogsSince(after model.LogLineID, limit int) []model.LogLine
}

type TraceRecording interface {
	Append(chunk model.TraceChunk)
	Latest() (model.TraceChunk, bool)
	At(t time.Time) (model.TraceChunk, bool)
	// Between は [from, to] に重なる区切りを古い順に返す
	Between(from, to time.Time) []model.TraceChunk
	FindLogMark(seq uint64) (model.TraceChunk, model.TraceLogMark, bool)
	// Span は今持てている区間と、その大きさ。持てる時間は trace の量 (対象の忙しさ) で変わる
	Span() (model.RecordingSpan, bool)
}

type LogRecording interface {
	Append(line model.LogLine) model.LogLine
	Snapshot() []model.LogLine
	Get(id model.LogLineID) (model.LogLine, bool)
}

type SavedRecordings interface {
	// Save は録画を保存する。同じ ID がすでにあるかは Has で確かめる
	Save(rec model.SavedRecording, chunks []model.TraceChunk) error
	Has(id string) bool
	List() []model.SavedRecording
	Chunks(id string) ([]model.TraceChunk, bool)
}

// AgentInbox は agent が push の接続で送ってきたもの (ログ・dump) の受け取り先。DumpSource・LogSource はここに届いたものを読む
type AgentInbox interface {
	Receive(logs []model.LogLine, dump *model.GoroutineDump)
}

// MetricStore は集計 (MetricPoint) の時系列の置き場。同じ区切りに足されたものは Merge して持つ。
// 期間に応じて細かさの違う区切りで持ち、Range は step を描けるいちばん細かいものを返す
type MetricStore interface {
	Add(points ...MetricPoint)
	// Range は [from, to] に入る区切りを古い順に、map を共有しない複製で返す
	Range(from, to time.Time, step time.Duration) []MetricPoint
	// Since は持っているいちばん古い区切り
	Since() (time.Time, bool)
	// Flush は閉じた区切り (now より前に終わったもの) をまだ書いていなければ書き、保持期間を過ぎたものを捨てる
	Flush(now time.Time) error
}
