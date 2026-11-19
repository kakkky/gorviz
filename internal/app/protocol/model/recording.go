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

// JoinChunks は時刻の順に並んだ区切りから [from, to] に当たる部分を切り出し、1 つの Timeline につなぐ。startedAt はつないだ Timeline の時刻 0。
// 後ろの区切りの先頭の状態 (t=0 のイベント) は、その区切りの始まりの時刻に置き直す。区切りの間のすき間 (取り直しの HTTP の往復) には何も起きない。
// M の番号は区切りごとに現れた順に振り直しているので、境目をまたいで同じ番号が同じ OS スレッドとは限らない
func JoinChunks(chunks []TraceChunk, from, to time.Time) (tl SchedulerTimeline, startedAt time.Time, ok bool) {
	lanes := map[ProcID]int{}
	threads := map[ThreadID]bool{}
	for _, c := range chunks {
		lo, hi := max(c.Offset(from), 0), min(c.Offset(to), c.Timeline.Window)
		if hi <= lo {
			continue
		}
		part := c.Timeline.Slice(lo, hi)
		partStart := c.StartedAt.Add(lo)
		if !ok {
			tl, startedAt, ok = SchedulerTimeline{Entries: map[GoroutineID]string{}}, partStart, true
		}
		shift := partStart.Sub(startedAt)
		for _, ev := range part.Events {
			ev.At += shift
			tl.Events = append(tl.Events, ev)
		}
		for _, r := range part.GC {
			r.Start, r.End = r.Start+shift, r.End+shift
			tl.GC = append(tl.GC, r)
		}
		for _, h := range part.Heap {
			h.At += shift
			tl.Heap = append(tl.Heap, h)
		}
		for _, e := range part.GCEnds {
			e.At += shift
			tl.GCEnds = append(tl.GCEnds, e)
		}
		for _, l := range part.Procs {
			i, seen := lanes[l.Proc]
			if !seen {
				i = len(tl.Procs)
				lanes[l.Proc] = i
				tl.Procs = append(tl.Procs, ProcLane{Proc: l.Proc})
			}
			for _, s := range l.Spans {
				s.Start, s.End = s.Start+shift, s.End+shift
				tl.Procs[i].Spans = append(tl.Procs[i].Spans, s)
			}
		}
		for _, th := range part.Threads {
			if !threads[th] {
				threads[th] = true
				tl.Threads = append(tl.Threads, th)
			}
		}
		for g, e := range part.Entries {
			if _, has := tl.Entries[g]; !has {
				tl.Entries[g] = e
			}
		}
		tl.GoroutinesCreated += part.GoroutinesCreated
		tl.GoroutinesDestroyed += part.GoroutinesDestroyed
		tl.GCCycles += part.GCCycles
		tl.EventsTruncated = tl.EventsTruncated || part.EventsTruncated
		tl.Window = shift + part.Window
	}
	return tl, startedAt, ok
}
