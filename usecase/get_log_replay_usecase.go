package usecase

import (
	"context"
	"maps"
	"time"

	"github.com/kakkky/gorviz/domain"
)

const logLocateSlack = 5 * time.Second

type GetLogReplayUsecase struct {
	logRecording   domain.LogRecording
	traceRecording domain.TraceRecording
	inMemoryKVS    domain.InMemoryKVS
}

func (us *UseCase) NewGetLogReplayUsecase() *GetLogReplayUsecase {
	return &GetLogReplayUsecase{logRecording: us.logRecording, traceRecording: us.traceRecording, inMemoryKVS: us.inMemoryKVS}
}

type GetLogReplayUsecaseInput struct {
	LogID  domain.LogLineID
	Before time.Duration
	After  time.Duration
}

type GetLogReplayUsecaseOutput struct {
	Line     domain.LogLine
	Timeline domain.SchedulerTimeline
	// WindowStartedAt は Timeline の時刻 0 に当たる壁時計の時刻
	WindowStartedAt time.Time
	// Focus はクリックしたログの、再生区間の中での位置。Exact はログを書いた G まで trace で特定できたか
	Focus      GetLogReplayUsecaseLogMark
	WindowLogs []GetLogReplayUsecaseLogMark
	Leaked     []domain.GoroutineID
}

type GetLogReplayUsecaseLogMark struct {
	Line      domain.LogLine
	At        time.Duration
	Goroutine domain.GoroutineID
	Exact     bool
}

// Exec はログ 1 行の時刻の前後を、録画した trace から切り出す。
// ログが gorviz の slog ハンドラ経由なら trace に残った印で時刻と G を正確に決め、そうでなければログの時刻で位置を決める。
func (us *GetLogReplayUsecase) Exec(_ context.Context, in GetLogReplayUsecaseInput) (GetLogReplayUsecaseOutput, error) {
	line, ok := us.logRecording.Get(in.LogID)
	if !ok {
		return GetLogReplayUsecaseOutput{}, domain.ErrNotFound.With("the log line is no longer kept")
	}
	chunk, focus, ok := us.locate(line)
	if !ok {
		return GetLogReplayUsecaseOutput{}, domain.ErrNotFound.With("the trace around this log is not recorded (outside the retention, or before recording started)")
	}

	from := max(focus.At-in.Before, 0)
	timeline := chunk.Timeline.Slice(from, focus.At+in.After)
	timeline.Entries = maps.Clone(timeline.Entries)
	goroutines := getOr(us.inMemoryKVS, latestGoroutinesKey, []domain.Goroutine(nil))
	timeline.EnrichFromDump(goroutines)

	out := GetLogReplayUsecaseOutput{
		Line:            line,
		Timeline:        timeline,
		WindowStartedAt: chunk.StartedAt.Add(from),
		Focus:           GetLogReplayUsecaseLogMark{Line: line, At: focus.At - from, Goroutine: focus.Goroutine, Exact: focus.Exact},
	}
	windowStart, windowEnd := out.WindowStartedAt, out.WindowStartedAt.Add(timeline.Window)
	for _, l := range us.logRecording.Snapshot() {
		// 全行を trace と突き合わせると重いので、ログに書かれた時刻で先に絞る。印の付いた行もログの時刻は書いた瞬間に近いので、余裕を持たせて絞る
		if l.At.Before(windowStart.Add(-logLocateSlack)) || l.At.After(windowEnd.Add(logLocateSlack)) {
			continue
		}
		c, m, ok := us.locate(l)
		if !ok || !c.StartedAt.Equal(chunk.StartedAt) || m.At < from || m.At > from+timeline.Window {
			continue
		}
		m.At -= from
		out.WindowLogs = append(out.WindowLogs, m)
	}
	for _, g := range goroutines {
		if g.Leaked {
			out.Leaked = append(out.Leaked, g.ID)
		}
	}
	return out, nil
}

// locate はログ 1 行が録画のどの区切りのどの時刻に当たるかを返す。At は区切りの先頭からの時刻
func (us *GetLogReplayUsecase) locate(line domain.LogLine) (domain.TraceChunk, GetLogReplayUsecaseLogMark, bool) {
	if line.HasTraceSeq {
		if chunk, mark, ok := us.traceRecording.FindLogMark(line.TraceSeq); ok {
			return chunk, GetLogReplayUsecaseLogMark{Line: line, At: mark.At, Goroutine: mark.Goroutine, Exact: true}, true
		}
	}
	chunk, ok := us.traceRecording.At(line.At)
	if !ok {
		return domain.TraceChunk{}, GetLogReplayUsecaseLogMark{}, false
	}
	return chunk, GetLogReplayUsecaseLogMark{Line: line, At: chunk.Offset(line.At)}, true
}
