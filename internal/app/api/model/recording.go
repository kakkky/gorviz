package model

import "time"

// TraceChunk は録画の 1 区切り。trace は同時に 1 本しか取れないので、録画ループが区切りを間を空けずに取り続け、
// Live も Explore もここから読む。
type TraceChunk struct {
	// StartedAt は Timeline の時刻 0 に当たる壁時計の時刻。trace の時計とログの時刻を対応づけるのに使う
	StartedAt time.Time
	Timeline  SchedulerTimeline
	LogMarks  []TraceLogMark
	Stats     TraceStats
	// Raw は取ったままの trace。`go tool trace` で開けるように書き出したり、保存したりするのに使う
	Raw []byte
}

// schedEventBytes は読み込んだ再生用イベント 1 つがメモリで占める大きさの目安
const schedEventBytes = 96

// SizeBytes はリングバッファの上限に数える大きさ。生の trace と、読み込んだイベントの両方を数える
func (c TraceChunk) SizeBytes() int64 {
	return int64(len(c.Raw)) + int64(len(c.Timeline.Events))*schedEventBytes
}

const (
	// LogMarkCategory は gorviz のロガーの差し込み口が trace.Log に渡すカテゴリ
	LogMarkCategory = "gorviz.log"
	// LogSeqAttrKey は gorviz のロガーの差し込み口がログ行に足す属性のキー。同じ番号を trace にも書く
	LogSeqAttrKey = "gorviz_seq"
)

// TraceLogMark は対象が gorviz のロガーの差し込み口経由でログを書いた瞬間の記録。どの G がいつ書いたかが正確に分かる
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

// Since は t より前の部分を落とした区切りを返す。FlightRecorder から切り出すと前の区切りと重なるので、重なりを落とすのに使う。
// t の時点の状態は先頭に積むので、残った部分だけで再生や内訳の計算ができる
func (c TraceChunk) Since(t time.Time) TraceChunk {
	from := c.Offset(t)
	if from <= 0 {
		return c
	}
	out := c
	out.StartedAt = c.StartedAt.Add(from)
	out.Timeline = c.Timeline.Slice(from, c.Timeline.Window)
	out.LogMarks = nil
	for _, m := range c.LogMarks {
		if m.At >= from {
			m.At -= from
			out.LogMarks = append(out.LogMarks, m)
		}
	}
	return out
}

// inheritGap は InheritWaitReasons で、前の区切りの状態を引き継ぐ区切りの間のすき間の上限
const inheritGap = 3 * time.Second

// InheritWaitReasons は待機理由もスタックも載らなかった待ちのイベントを、同じ G の直前の待ちから補う。
// runtime は窓より前から続いている待ち (区切りの先頭に持ち越した状態や途中の書き出し) の理由を書かない (unknown) ので、
// 区切りを順に貯める側が前の区切り prev の最後の状態を引き継ぐ。prev が空か、間が inheritGap より空いていれば区切りの中だけで補う。
// 区切りの間のすき間で G が別の待ちに移っていても区別できないので、待ちが続いていれば同じ待ちとみなす
func (c *TraceChunk) InheritWaitReasons(prev TraceChunk) {
	last := map[GoroutineID]SchedEvent{}
	if len(prev.Timeline.Events) > 0 && c.StartedAt.Sub(prev.EndedAt()) <= inheritGap {
		for _, ev := range prev.Timeline.Events {
			if ev.Kind == SchedEventGoroutine {
				last[ev.Goroutine] = ev
			}
		}
	}
	for i := range c.Timeline.Events {
		ev := &c.Timeline.Events[i]
		if ev.Kind != SchedEventGoroutine {
			continue
		}
		if before, ok := last[ev.Goroutine]; ok && ev.Place.Kind == PlaceWaiting && before.Place.Kind == PlaceWaiting {
			if !knownWaitReason(ev.Place.Reason) && knownWaitReason(before.Place.Reason) {
				ev.Place.Reason = before.Place.Reason
			}
			if ev.Function == "" {
				ev.Function, ev.Location = before.Function, before.Location
			}
		}
		last[ev.Goroutine] = *ev
	}
}

func knownWaitReason(r string) bool {
	return r != "" && r != UnknownWaitReason
}
