package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// IndexAppRuntimeTraceUsecase は、取った順に届く trace の区切りを、trace が閉じるまで読み続け、1 秒が閉じるたびに Observation にして入れる。
// 読み手と組み立ての途中の状態は Exec の中で持ち、呼び出しをまたいで持たない
type IndexAppRuntimeTraceUsecase struct {
	sessions     domain.SessionStore
	observations domain.ObservationAccessor
	stream       domain.TraceStream
}

type IndexAppRuntimeTraceUsecaseInput struct{}

// IndexAppRuntimeTraceUsecaseOutput の Unreadable は、途中で切れていて読めたところまでを使った区切りの数
type IndexAppRuntimeTraceUsecaseOutput struct {
	Chunks     int
	Unreadable int
}

func (us *UseCase) NewIndexAppRuntimeTraceUsecase() *IndexAppRuntimeTraceUsecase {
	return &IndexAppRuntimeTraceUsecase{sessions: us.sessions, observations: us.observations, stream: us.stream}
}

func (u *IndexAppRuntimeTraceUsecase) Exec(_ context.Context, _ IndexAppRuntimeTraceUsecaseInput) (IndexAppRuntimeTraceUsecaseOutput, error) {
	var out IndexAppRuntimeTraceUsecaseOutput
	b := domain.NewObservationBuilder()
	for {
		c, ok := u.stream.NextTrace()
		if !ok {
			break
		}
		out.Chunks++
		events, err := domain.DecodeTraceChunk(c.Trace)
		if err != nil {
			out.Unreadable++
		}
		started := b.Started()
		closed := b.ApplyChunk(events, c.Threads)
		if !started && b.Started() {
			u.sessions.UpdateSession(func(s *domain.Session) { s.Observed.Started = true })
		}
		for _, o := range closed {
			if err := u.put(o); err != nil {
				return out, err
			}
		}
		if clock := b.Clock(); clock != u.sessions.Session().Observed.Clock {
			u.sessions.UpdateSession(func(s *domain.Session) { s.Observed.Clock = clock })
		}
	}
	started := b.Started()
	last, until := b.End()
	if started {
		if err := u.put(last); err != nil {
			return out, err
		}
	}
	// アプリが終わるとき、取りかけの区切りは届かないので、trace の末尾はいつも欠ける
	u.sessions.UpdateSession(func(s *domain.Session) {
		s.Observed.Until, s.Observed.Ended, s.Observed.Broken = max(s.Observed.Until, until), true, started
	})
	return out, nil
}

func (u *IndexAppRuntimeTraceUsecase) put(o domain.Observation) error {
	if err := u.observations.Put(o); err != nil {
		return err
	}
	u.sessions.UpdateSession(func(s *domain.Session) {
		s.Observed.Until = max(s.Observed.Until, o.To())
		if s.Observed.Procs == 0 {
			s.Observed.Procs, s.App.Procs = o.Procs, o.Procs
		}
	})
	return nil
}
