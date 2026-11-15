package usecase

import (
	"context"
	"maps"
	"time"

	"github.com/kakkky/gorviz/domain"
)

type GetGoroutineUsecase struct {
	traceRecording domain.TraceRecording
	inMemoryKVS    domain.InMemoryKVS
}

func (us *UseCase) NewGetGoroutineUsecase() *GetGoroutineUsecase {
	return &GetGoroutineUsecase{traceRecording: us.traceRecording, inMemoryKVS: us.inMemoryKVS}
}

// GetGoroutineUsecaseInput は 1 つの G を [From, To] の区間で見る。At はその中の注目する時点 (ログを書いた瞬間など) で、
// GMP 再生はその前後 ReplayHalf ずつを見せる
type GetGoroutineUsecaseInput struct {
	Goroutine  domain.GoroutineID
	At         time.Time
	From       time.Time
	To         time.Time
	ReplayHalf time.Duration
}

type GetGoroutineUsecaseOutput struct {
	Goroutine domain.GoroutineID
	At        time.Time
	From      time.Time
	To        time.Time
	Entry     string
	// Dump は直近の goroutine dump でのこの G。dump にいなければ HasDump は false
	Dump    domain.Goroutine
	HasDump bool
	// 診断: 主因と、At の時点で止まっていた場所
	Breakdown  domain.StateBreakdown
	Primary    domain.PrimaryCause
	HasPrimary bool
	Certainty  domain.Certainty
	Function   string
	Location   string
	WaitReason string
	// 根拠: At の前後の GMP 再生。録画がなければ HasReplay は false
	Replay          domain.SchedulerTimeline
	ReplayStartedAt time.Time
	ReplayFocus     time.Duration
	HasReplay       bool
	Leaked          []domain.GoroutineID
}

// Exec は区間に重なる録画の区切りをすべて読み、G の時間を主因ごとに合計する。
// 録画がない区間しかなければ、直近の dump の状態から主因を推し量る (INFERENCE)
func (us *GetGoroutineUsecase) Exec(_ context.Context, in GetGoroutineUsecaseInput) (GetGoroutineUsecaseOutput, error) {
	if !in.To.After(in.From) {
		return GetGoroutineUsecaseOutput{}, domain.ErrInvalidArgument.With("the interval is empty")
	}
	out := GetGoroutineUsecaseOutput{Goroutine: in.Goroutine, At: in.At, From: in.From, To: in.To, Breakdown: domain.NewStateBreakdown()}

	goroutines := getOr(us.inMemoryKVS, latestGoroutinesKey, []domain.Goroutine(nil))
	for _, g := range goroutines {
		if g.ID == in.Goroutine {
			out.Dump, out.HasDump, out.Entry = g, true, g.EntryFunction
		}
		if g.Leaked {
			out.Leaked = append(out.Leaked, g.ID)
		}
	}

	for _, c := range us.traceRecording.Between(in.From, in.To) {
		from, to := c.Offset(laterOf(in.From, c.StartedAt)), c.Offset(earlierOf(in.To, c.EndedAt()))
		out.Breakdown.Add(c.Timeline.Breakdown(in.Goroutine, from, to))
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

	if c, ok := us.traceRecording.At(in.At); ok {
		at := c.Offset(in.At)
		out.Function, out.Location, out.WaitReason = lastKnownPlace(c.Timeline, in.Goroutine, at)
		from := max(at-in.ReplayHalf, 0)
		out.Replay = c.Timeline.Slice(from, at+in.ReplayHalf)
		out.Replay.Entries = maps.Clone(out.Replay.Entries)
		out.Replay.EnrichFromDump(goroutines)
		out.ReplayStartedAt, out.ReplayFocus, out.HasReplay = c.StartedAt.Add(from), at-from, true
	}
	if out.Function == "" && out.HasDump {
		out.Function = out.Dump.CurrentFunction
		if out.Dump.StateCategory() == domain.GoroutineWaiting {
			out.WaitReason = out.Dump.State
		}
	}
	return out, nil
}

// lastKnownPlace は at 以前で最後に分かった、G のいた関数・位置と、その時点で待機中なら待機理由を返す
func lastKnownPlace(tl domain.SchedulerTimeline, g domain.GoroutineID, at time.Duration) (function, location, reason string) {
	for _, ev := range tl.Events {
		if ev.At > at {
			break
		}
		if ev.Kind != domain.SchedEventGoroutine || ev.Goroutine != g {
			continue
		}
		if ev.Function != "" {
			function, location = ev.Function, ev.Location
		}
		reason = ""
		if ev.Place.Kind == domain.PlaceWaiting {
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
