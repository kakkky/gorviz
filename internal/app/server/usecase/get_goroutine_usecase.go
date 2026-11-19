package usecase

import (
	"context"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

type GetGoroutineUsecase struct {
	traceRecording domain.TraceRecording
	inMemoryKVS    domain.InMemoryKVS
	logs           logLocator
}

func (us *UseCase) NewGetGoroutineUsecase() *GetGoroutineUsecase {
	return &GetGoroutineUsecase{traceRecording: us.traceRecording, inMemoryKVS: us.inMemoryKVS, logs: logLocator{traceRecording: us.traceRecording, logRecording: us.logRecording}}
}

// GetGoroutineUsecaseInput は 1 つの G を [From, To] の区間で見る。At はその中の注目する時点 (ログを書いた瞬間など) で、
// GMP 再生はその前後 ReplayHalf ずつを見せる
type GetGoroutineUsecaseInput struct {
	Goroutine  model.GoroutineID
	At         time.Time
	From       time.Time
	To         time.Time
	ReplayHalf time.Duration
}

type GetGoroutineUsecaseOutput struct {
	Goroutine model.GoroutineID
	At        time.Time
	From      time.Time
	To        time.Time
	Entry     string
	// Dump は直近の goroutine dump でのこの G。dump にいなければ HasDump は false
	Dump    model.Goroutine
	HasDump bool
	// 診断: 主因と、At の時点で止まっていた場所
	Breakdown domain.StateBreakdown
	// Causes は Breakdown を表示の順に並べたもの
	Causes     []GoroutineCauseDuration
	Primary    domain.PrimaryCause
	HasPrimary bool
	Certainty  domain.Certainty
	Function   string
	Location   string
	WaitReason string
	// Longest は区間の中で主因の状態を途切れずに一番長く続けた 1 回。待ちなら誰が起こしたかも添える
	Longest    GoroutineStretch
	HasLongest bool
	// 根拠: At の前後の GMP 再生。録画がなければ HasReplay は false
	Replay          model.SchedulerTimeline
	ReplayStartedAt time.Time
	ReplayFocus     time.Duration
	HasReplay       bool
	// ReplayLogs は再生の窓に当たるログ
	ReplayLogs []LogMark
	Leaked     []model.GoroutineID
	// StateSince は再生の窓の頭 (再生がなければ At) の時点の状態 (StateSinceCause) に入った時刻。窓の中の状態の変わり目は再生から分かるので、
	// 窓より前から続いている状態の始まりだけを録画からさかのぼって探す。HasStateSince が false で StateSinceCause があれば、
	// RecordedFrom (さかのぼれた録画の始まり) より前から続いている。StateSinceCause もなければ、その時点までにこの G は録画に現れていない
	StateSince      time.Time
	StateSinceCause domain.PrimaryCause
	HasStateSince   bool
	RecordedFrom    time.Time
}

// GoroutineStretch は G が 1 つの主因の状態を続けた 1 回。Ended が false なら区間の終わりまで続いていた。
// HasWake は待ち (waiting) から出たときに立つ。Waker が 0 なら、別の G ではなく runtime (タイマー・ネットワークのポーラー) が起こした
type GoroutineStretch struct {
	From       time.Time
	To         time.Time
	Function   string
	Location   string
	WaitReason string
	Ended      bool
	HasWake    bool
	Waker      model.GoroutineID
	// WakerFunction・WakerLocation は起こしたときに Waker がいた関数と位置 (Unlock・channel の送信など)
	WakerFunction string
	WakerLocation string
	WakerEntry    string
	// WakerBreakdown はこの G が待っていた間の Waker の内訳。WakerBusy* はそのうち Waker の主因を一番長く続けた 1 回の場所
	WakerBreakdown    domain.StateBreakdown
	WakerPrimary      domain.PrimaryCause
	HasWakerPrimary   bool
	WakerBusyFunction string
	WakerBusyLocation string
}

type GoroutineCauseDuration struct {
	Cause    domain.PrimaryCause
	Duration time.Duration
	// Primary は記録から言える (FACT) 主因のとき true。dump から推し量った主因 (INFERENCE) では立てない
	Primary bool
}

// Exec は区間に重なる録画の区切りをすべて読み、G の時間を主因ごとに合計する。
// 録画がない区間しかなければ、直近の dump の状態から主因を推し量る (INFERENCE)
func (us *GetGoroutineUsecase) Exec(_ context.Context, in GetGoroutineUsecaseInput) (GetGoroutineUsecaseOutput, error) {
	if !in.To.After(in.From) {
		return GetGoroutineUsecaseOutput{}, model.ErrInvalidArgument.With("the interval is empty")
	}
	out := GetGoroutineUsecaseOutput{Goroutine: in.Goroutine, At: in.At, From: in.From, To: in.To, Breakdown: domain.NewStateBreakdown()}

	goroutines := domain.KVSGetOr(us.inMemoryKVS, latestGoroutinesKey, []model.Goroutine(nil))
	for _, g := range goroutines {
		if g.ID == in.Goroutine {
			out.Dump, out.HasDump, out.Entry = g, true, g.EntryFunction
		}
		if g.Leaked {
			out.Leaked = append(out.Leaked, g.ID)
		}
	}

	chunks := us.traceRecording.Between(in.From, in.To)
	for _, c := range chunks {
		from, to := c.Offset(laterOf(in.From, c.StartedAt)), c.Offset(earlierOf(in.To, c.EndedAt()))
		out.Breakdown.Add(domain.Breakdown(c.Timeline, in.Goroutine, from, to))
		if out.Entry == "" {
			out.Entry = c.Timeline.Entries[in.Goroutine]
		}
	}
	out.Breakdown.Unrecorded = in.To.Sub(in.From) - out.Breakdown.Recorded()

	out.Primary, out.HasPrimary = out.Breakdown.Primary()
	out.Certainty = domain.CertaintyFact
	if !out.HasPrimary && out.HasDump {
		out.Primary, out.HasPrimary, out.Certainty = domain.CauseOfGoroutine(out.Dump), true, domain.CertaintyInference
	}

	for _, c := range domain.PrimaryCauses {
		out.Causes = append(out.Causes, GoroutineCauseDuration{Cause: c, Duration: out.Breakdown.Durations[c], Primary: out.HasPrimary && c == out.Primary && out.Certainty == domain.CertaintyFact})
	}

	if out.HasPrimary && out.Certainty == domain.CertaintyFact {
		out.Longest, out.HasLongest = longestStretch(chunks, in.Goroutine, out.Primary, in.From, in.To)
		if out.HasLongest && out.Longest.HasWake && out.Longest.Waker != 0 {
			addWaker(&out.Longest, chunks, goroutines)
		}
		if out.HasLongest && out.Longest.Function != "" {
			out.Function, out.Location, out.WaitReason = out.Longest.Function, out.Longest.Location, out.Longest.WaitReason
		}
	}
	if out.HasPrimary && out.Certainty == domain.CertaintyFact && out.Function == "" {
		// 止まっていた場所は、主因の状態に入ったときの場所。区間の中で最後に入ったものを使う
		for _, c := range slices.Backward(chunks) {
			if ev, ok := domain.PlaceOfCause(c.Timeline, in.Goroutine, out.Primary, c.Offset(earlierOf(in.To, c.EndedAt()))); ok {
				out.Function, out.Location, out.WaitReason = ev.Function, ev.Location, ev.Place.Reason
				break
			}
		}
	}
	// 再生は At の前後 ReplayHalf ずつに、診断の区間 [From, To] も含める (再生の内訳と診断の区間をそろえる)。
	// 区切りをまたぐときは前後の区切りをつなぐ
	replayFrom, replayTo := earlierOf(in.From, in.At.Add(-in.ReplayHalf)), laterOf(in.To, in.At.Add(in.ReplayHalf))
	replayFrom = laterOf(replayFrom, replayTo.Add(-maxReplaySpan))
	if tl, start, ok := model.JoinChunks(us.traceRecording.Between(replayFrom, replayTo), replayFrom, replayTo); ok && !in.At.Before(start) && !in.At.After(start.Add(tl.Window)) {
		at := in.At.Sub(start)
		if out.Function == "" {
			out.Function, out.Location, out.WaitReason = lastKnownPlace(tl, in.Goroutine, at)
		}
		out.Replay = tl
		domain.EnrichFromDump(&out.Replay, goroutines)
		out.ReplayStartedAt, out.ReplayFocus, out.HasReplay = start, at, true
		out.ReplayLogs = us.logs.marksBetween(out.ReplayStartedAt, out.ReplayStartedAt.Add(out.Replay.Window))
	}
	base := in.At
	if out.HasReplay {
		base = out.ReplayStartedAt
	}
	out.StateSince, out.StateSinceCause, out.HasStateSince, out.RecordedFrom = us.stateSince(in.Goroutine, base)
	// 窓より前から待機している G は trace に理由もスタックも載らないので、dump で補う
	if out.HasDump && out.Dump.StateCategory() == model.GoroutineWaiting {
		if out.Function == "" {
			out.Function = out.Dump.CurrentFunction
		}
		if out.WaitReason == "" || out.WaitReason == model.UnknownWaitReason {
			out.WaitReason = out.Dump.State
		}
	}
	if out.Function == "" && out.HasDump {
		out.Function = out.Dump.CurrentFunction
	}
	return out, nil
}

// longestStretch は区間に重なる区切りから、g が cause の状態を途切れずに続けた一番長い 1 回を探す。
// 区切りの終わりまで続いた状態が次の区切りの頭から続いていれば、1 回としてつなぐ (待ちが解けたのは次の区切りに載る)
func longestStretch(chunks []model.TraceChunk, g model.GoroutineID, cause domain.PrimaryCause, from, to time.Time) (GoroutineStretch, bool) {
	type stretch struct {
		from, to time.Time
		st       domain.CauseStretch
		// open は区切りの終わりまで続いていたこと (区切りの外で解けたかもしれない)
		open bool
	}
	var all []stretch
	for _, c := range chunks {
		lo, hi := c.Offset(laterOf(from, c.StartedAt)), c.Offset(earlierOf(to, c.EndedAt()))
		for _, st := range domain.Stretches(c.Timeline, g, cause, lo, hi) {
			cur := stretch{from: c.StartedAt.Add(st.Start), to: c.StartedAt.Add(st.End), st: st, open: !st.HasLeave && st.End == c.Timeline.Window}
			if n := len(all); n > 0 && all[n-1].open && st.Start == 0 && cur.from.Sub(all[n-1].to) <= chunkGapProbes[len(chunkGapProbes)-1] {
				prev := all[n-1]
				cur.from, cur.st.Enter = prev.from, prev.st.Enter
				if cur.st.Enter.Function == "" {
					cur.st.Enter = st.Enter
				}
				all[n-1] = cur
				continue
			}
			all = append(all, cur)
		}
	}
	if len(all) == 0 {
		return GoroutineStretch{}, false
	}
	best := all[0]
	for _, s := range all[1:] {
		if s.to.Sub(s.from) > best.to.Sub(best.from) {
			best = s
		}
	}
	out := GoroutineStretch{From: best.from, To: best.to, Function: best.st.Enter.Function, Location: best.st.Enter.Location, Ended: best.st.HasLeave}
	if best.st.Enter.Place.Kind == model.PlaceWaiting {
		out.WaitReason = best.st.Enter.Place.Reason
		if leave := best.st.Leave; best.st.HasLeave && (leave.Place.Kind == model.PlaceLocalQueue || leave.Place.Kind == model.PlaceGlobalQueue) {
			out.HasWake = true
			out.Waker, out.WakerFunction, out.WakerLocation = leave.WokenBy, leave.WokenByFunction, leave.WokenByLocation
		}
	}
	return out, true
}

// addWaker は待っていた間 (s.From〜s.To) に、起こした G が何をしていたかを足す
func addWaker(s *GoroutineStretch, chunks []model.TraceChunk, goroutines []model.Goroutine) {
	s.WakerBreakdown = domain.NewStateBreakdown()
	for _, c := range chunks {
		if !c.EndedAt().After(s.From) || c.StartedAt.After(s.To) {
			continue
		}
		s.WakerBreakdown.Add(domain.Breakdown(c.Timeline, s.Waker, c.Offset(laterOf(s.From, c.StartedAt)), c.Offset(earlierOf(s.To, c.EndedAt()))))
		if s.WakerEntry == "" {
			s.WakerEntry = c.Timeline.Entries[s.Waker]
		}
	}
	for _, g := range goroutines {
		if g.ID == s.Waker && s.WakerEntry == "" {
			s.WakerEntry = g.EntryFunction
		}
	}
	s.WakerPrimary, s.HasWakerPrimary = s.WakerBreakdown.Primary()
	if s.HasWakerPrimary {
		if busy, ok := longestStretch(chunks, s.Waker, s.WakerPrimary, s.From, s.To); ok {
			s.WakerBusyFunction, s.WakerBusyLocation = busy.Function, busy.Location
		}
	}
}

// lastKnownPlace は at 以前で最後に分かった、G のいた関数・位置と、その時点で待機中なら待機理由を返す
func lastKnownPlace(tl model.SchedulerTimeline, g model.GoroutineID, at time.Duration) (function, location, reason string) {
	for _, ev := range tl.Events {
		if ev.At > at {
			break
		}
		if ev.Kind != model.SchedEventGoroutine || ev.Goroutine != g {
			continue
		}
		if ev.Function != "" {
			function, location = ev.Function, ev.Location
		}
		reason = ""
		if ev.Place.Kind == model.PlaceWaiting {
			reason = ev.Place.Reason
		}
	}
	return function, location, reason
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlierOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// maxReplaySpan は再生につなぐ長さの上限。診断の区間が長くても (時間のかかったリクエスト)、ブラウザに送る再生が重くなりすぎないようにする
const maxReplaySpan = 10 * time.Second

// stateSinceMaxChunks はさかのぼる区切りの数の上限。録画が長くても画面を開く時間を抑える
const stateSinceMaxChunks = 1000

// chunkGapProbes は 1 つ前の区切りを探すときに、区切りの始まりから戻ってみる長さ。
// 対象の pprof から trace を順に取る agent では、取り直しのあいだ (HTTP の往復と解析) だけ区切りの間が空く
var chunkGapProbes = []time.Duration{time.Nanosecond, 10 * time.Millisecond, 100 * time.Millisecond, 500 * time.Millisecond, time.Second, 3 * time.Second}

// stateSince は at から区切りを 1 つずつさかのぼり、G が今の状態に入った時刻を探す。
// 録画をまとめて読む (Between) と生の trace ごと運ぶので重い。区切りは At で 1 つずつ読み、答えが出たらやめる。
// 区切りの間が空いていても数秒までは飛び越え、間をはさんで同じ状態なら続いていたとみなす。
// recordedFrom はさかのぼれたいちばん古い区切りの始まり
func (us *GetGoroutineUsecase) stateSince(g model.GoroutineID, at time.Time) (since time.Time, cause domain.PrimaryCause, ok bool, recordedFrom time.Time) {
	scan := domain.NewStateSinceScan(g, at)
	c, found := us.traceRecording.At(at)
	for range stateSinceMaxChunks {
		if !found {
			break
		}
		recordedFrom = c.StartedAt
		if scan.Add(c) {
			break
		}
		c, found = us.previousChunk(c)
	}
	since, cause, ok = scan.Result()
	return since, cause, ok, recordedFrom
}

// previousChunk は c の 1 つ前の区切り。間が空いていれば、少しずつ前に戻って探す
func (us *GetGoroutineUsecase) previousChunk(c model.TraceChunk) (model.TraceChunk, bool) {
	for _, back := range chunkGapProbes {
		if prev, ok := us.traceRecording.At(c.StartedAt.Add(-back)); ok && prev.StartedAt.Before(c.StartedAt) {
			return prev, true
		}
	}
	return model.TraceChunk{}, false
}
