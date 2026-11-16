package usecase

import (
	"context"
	"maps"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

type GetSchedulerReplayUsecase struct {
	traceRecording domain.TraceRecording
	inMemoryKVS    domain.InMemoryKVS
}

func (us *UseCase) NewGetSchedulerReplayUsecase() *GetSchedulerReplayUsecase {
	return &GetSchedulerReplayUsecase{traceRecording: us.traceRecording, inMemoryKVS: us.inMemoryKVS}
}

// GetSchedulerReplayUsecaseInput の Length は再生する長さ。録画の区切りより長ければ、区切りの全体になる。
// At が空なら最新の区切りの末尾を、あれば At を真ん中にした区間を切り出す
type GetSchedulerReplayUsecaseInput struct {
	At     time.Time
	Length time.Duration
}

type GetSchedulerReplayUsecaseOutput struct {
	Timeline model.SchedulerTimeline
	// StartedAt は Timeline の時刻 0 に当たる壁時計の時刻
	StartedAt time.Time
	Leaked    []model.GoroutineID
}

// Exec は録画から Length だけ切り出す。区切りの端にかかるときは、長さを保つようずらす。録画にない時刻なら ErrNotFound
func (us *GetSchedulerReplayUsecase) Exec(_ context.Context, in GetSchedulerReplayUsecaseInput) (GetSchedulerReplayUsecaseOutput, error) {
	chunk, ok := us.traceRecording.Latest()
	if !in.At.IsZero() {
		chunk, ok = us.traceRecording.At(in.At)
	}
	if !ok {
		return GetSchedulerReplayUsecaseOutput{}, model.ErrNotFound.With("this time is not in the recording (nothing recorded yet, dropped from the ring buffer, or outside the intermittent recording)")
	}
	w := chunk.Timeline.Window
	from := max(w-in.Length, 0)
	if !in.At.IsZero() {
		from = min(max(chunk.Offset(in.At)-in.Length/2, 0), from)
	}
	timeline := chunk.Timeline.Slice(from, from+in.Length)
	timeline.Entries = maps.Clone(timeline.Entries)
	goroutines := domain.KVSGetOr(us.inMemoryKVS, latestGoroutinesKey, []model.Goroutine(nil))
	domain.EnrichFromDump(&timeline, goroutines)

	out := GetSchedulerReplayUsecaseOutput{Timeline: timeline, StartedAt: chunk.StartedAt.Add(from)}
	for _, g := range goroutines {
		if g.Leaked {
			out.Leaked = append(out.Leaked, g.ID)
		}
	}
	return out, nil
}
