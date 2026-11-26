package usecase

import (
	"context"
	"errors"
	"io"
	"maps"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// IndexAppRuntimeTraceUsecase は流れてくる execution trace を、trace が終わるまで読み続け、1 秒が閉じるたびに Observation にして入れる。
// 出来事と goroutineleak プロファイルを取った時刻は、秒が閉じるのを待たずにセッションへ足す。読み手と組み立ての途中の状態は Exec の中で持ち、呼び出しをまたいで持たない
type IndexAppRuntimeTraceUsecase struct {
	sessions     domain.SessionStore
	observations domain.ObservationAccessor
	stream       domain.TraceStream
}

type IndexAppRuntimeTraceUsecaseInput struct{}

type IndexAppRuntimeTraceUsecaseOutput struct {
	Broken bool
}

func (us *UseCase) NewIndexAppRuntimeTraceUsecase() *IndexAppRuntimeTraceUsecase {
	return &IndexAppRuntimeTraceUsecase{sessions: us.sessions, observations: us.observations, stream: us.stream}
}

func (u *IndexAppRuntimeTraceUsecase) Exec(_ context.Context, _ IndexAppRuntimeTraceUsecaseInput) (IndexAppRuntimeTraceUsecaseOutput, error) {
	dec, err := domain.NewTraceDecoder(u.stream.TraceReader())
	if err != nil {
		broken := u.stream.TraceBroken()
		u.sessions.UpdateSession(func(s *domain.Session) { s.Observed.Ended, s.Observed.Broken = true, broken })
		return IndexAppRuntimeTraceUsecaseOutput{Broken: broken}, nil
	}
	b := domain.NewObservationBuilder()
	for {
		ev, readErr := dec.Next()
		if readErr != nil {
			broken := !errors.Is(readErr, io.EOF) || u.stream.TraceBroken()
			last, until := b.End()
			if err := u.put(last); err != nil {
				return IndexAppRuntimeTraceUsecaseOutput{}, err
			}
			u.publish(b)
			u.sessions.UpdateSession(func(s *domain.Session) {
				s.Observed.Until, s.Observed.Ended, s.Observed.Broken = until, true, broken
			})
			return IndexAppRuntimeTraceUsecaseOutput{Broken: broken}, nil
		}
		started := b.Started()
		closed := b.Apply(ev)
		if !started {
			u.sessions.UpdateSession(func(s *domain.Session) { s.Observed.Started = true })
		}
		for _, o := range closed {
			if err := u.put(o); err != nil {
				return IndexAppRuntimeTraceUsecaseOutput{}, err
			}
		}
		u.publish(b)
	}
}

func (u *IndexAppRuntimeTraceUsecase) put(o domain.Observation) error {
	if err := u.observations.Put(o); err != nil {
		return err
	}
	u.sessions.UpdateSession(func(s *domain.Session) {
		s.Observed.Until = max(s.Observed.Until, o.To())
		if s.Observed.Procs == 0 {
			s.Observed.Procs = o.Procs
		}
	})
	return nil
}

func (u *IndexAppRuntimeTraceUsecase) publish(b *domain.ObservationBuilder) {
	marks, checks := b.TakeNews()
	if marks == nil && checks == nil {
		return
	}
	u.sessions.UpdateSession(func(s *domain.Session) {
		s.Observed.Marks = append(s.Observed.Marks, marks...)
		if s.Observed.LeakChecks == nil {
			s.Observed.LeakChecks = map[uint64]time.Duration{}
		} else {
			s.Observed.LeakChecks = maps.Clone(s.Observed.LeakChecks)
		}
		maps.Copy(s.Observed.LeakChecks, checks)
	})
}
