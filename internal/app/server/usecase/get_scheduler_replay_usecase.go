package usecase

import (
	"context"
	"maps"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

type GetSchedulerReplayUsecase struct {
	traceRecording domain.TraceRecording
	inMemoryKVS    domain.InMemoryKVS
	logs           logLocator
}

func (us *UseCase) NewGetSchedulerReplayUsecase() *GetSchedulerReplayUsecase {
	return &GetSchedulerReplayUsecase{traceRecording: us.traceRecording, inMemoryKVS: us.inMemoryKVS, logs: logLocator{traceRecording: us.traceRecording, logRecording: us.logRecording}}
}

// GetSchedulerReplayUsecaseInput の Length は再生する長さ。録画の区切りより長ければ、区切りの全体になる。
// After があれば、その時刻から (録画に隙間があれば、隙間の後の録画の頭から) 続く区間を、Before があればその時刻までの区間を切り出す
// (再生の続き・前を、つなげて見るため。区切りの端では Length より短くなる)。
// どちらもなく At があれば At を真ん中にした区間を、At もなければ最新の区切りの末尾を切り出す
type GetSchedulerReplayUsecaseInput struct {
	At     time.Time
	After  time.Time
	Before time.Time
	Length time.Duration
}

type GetSchedulerReplayUsecaseOutput struct {
	Timeline model.SchedulerTimeline
	// StartedAt は Timeline の時刻 0 に当たる壁時計の時刻
	StartedAt time.Time
	Leaked    []model.GoroutineID
	// Logs は切り出した区間に当たるログ
	Logs []LogMark
}

// Exec は録画から Length だけ切り出す。区切りの端にかかるときは、長さを保つようずらす。録画にない時刻なら ErrNotFound
func (us *GetSchedulerReplayUsecase) Exec(_ context.Context, in GetSchedulerReplayUsecaseInput) (GetSchedulerReplayUsecaseOutput, error) {
	chunk, from, to, ok := us.window(in)
	if !ok {
		return GetSchedulerReplayUsecaseOutput{}, model.ErrNotFound.With("this time is not in the recording (nothing recorded yet, dropped from the ring buffer, or outside the intermittent recording)")
	}
	timeline := chunk.Timeline.Slice(from, to)
	timeline.Entries = maps.Clone(timeline.Entries)
	goroutines := domain.KVSGetOr(us.inMemoryKVS, latestGoroutinesKey, []model.Goroutine(nil))
	domain.EnrichFromDump(&timeline, goroutines)

	out := GetSchedulerReplayUsecaseOutput{Timeline: timeline, StartedAt: chunk.StartedAt.Add(from)}
	out.Logs = us.logs.marksBetween(out.StartedAt, out.StartedAt.Add(timeline.Window))
	for _, g := range goroutines {
		if g.Leaked {
			out.Leaked = append(out.Leaked, g.ID)
		}
	}
	return out, nil
}

// adjacentProbes は、区切りの間の隙間を飛び越えて隣の区切りを探すときに、ずらしてみる長さ。
// 対象の pprof から trace を順に取る agent では、取り直しのあいだ (HTTP の往復と解析) だけ区切りの間が空く
var adjacentProbes = []time.Duration{0, time.Millisecond, 10 * time.Millisecond, 100 * time.Millisecond, 500 * time.Millisecond, time.Second, 3 * time.Second}

// window は切り出す区切りと、その中の区間 [from, to]
func (us *GetSchedulerReplayUsecase) window(in GetSchedulerReplayUsecaseInput) (model.TraceChunk, time.Duration, time.Duration, bool) {
	switch {
	case !in.After.IsZero():
		for _, d := range adjacentProbes {
			c, ok := us.traceRecording.At(in.After.Add(d))
			if !ok || !c.StartedAt.Add(c.Timeline.Window).After(in.After) {
				continue
			}
			from := max(c.Offset(in.After), 0)
			return c, from, min(from+in.Length, c.Timeline.Window), true
		}
	case !in.Before.IsZero():
		for _, d := range adjacentProbes {
			c, ok := us.traceRecording.At(in.Before.Add(-d - time.Nanosecond))
			if !ok || !c.StartedAt.Before(in.Before) {
				continue
			}
			to := min(c.Offset(in.Before), c.Timeline.Window)
			return c, max(to-in.Length, 0), to, true
		}
	case !in.At.IsZero():
		c, ok := us.traceRecording.At(in.At)
		if !ok {
			break
		}
		w := c.Timeline.Window
		from := min(max(c.Offset(in.At)-in.Length/2, 0), max(w-in.Length, 0))
		return c, from, from + in.Length, true
	default:
		c, ok := us.traceRecording.Latest()
		if !ok {
			break
		}
		from := max(c.Timeline.Window-in.Length, 0)
		return c, from, from + in.Length, true
	}
	return model.TraceChunk{}, 0, 0, false
}
