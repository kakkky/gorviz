package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// GetGoroutineDetailUsecase は 1 つの goroutine が、期間の中で何に止まっていたかを返す。期間がなければ goroutine がいた期間 (直近 1 分まで)
type GetGoroutineDetailUsecase struct {
	sessions     domain.SessionStore
	observations domain.ObservationAccessor
	issues       domain.IssueBookStore
}

type GetGoroutineDetailUsecaseInput struct {
	ID       domain.GoroutineID
	From     time.Duration
	To       time.Duration
	HasRange bool
}

type GetGoroutineDetailUsecaseOutput struct {
	Detail domain.GoroutineDetail
}

func (us *UseCase) NewGetGoroutineDetailUsecase() *GetGoroutineDetailUsecase {
	return &GetGoroutineDetailUsecase{sessions: us.sessions, observations: us.observations, issues: us.issues}
}

func (u *GetGoroutineDetailUsecase) Exec(_ context.Context, in GetGoroutineDetailUsecaseInput) (GetGoroutineDetailUsecaseOutput, error) {
	if in.HasRange && in.To < in.From {
		return GetGoroutineDetailUsecaseOutput{}, domain.ErrInvalidArgument.With("the end of the range is before its start")
	}
	leaks := u.leaks()
	from, to := in.From, in.To
	if !in.HasRange {
		var err error
		if from, to, err = u.defaultRange(in.ID); err != nil {
			return GetGoroutineDetailUsecaseOutput{}, err
		}
	}
	set, err := observationSet(u.observations, u.sessions, from, to)
	if err != nil {
		return GetGoroutineDetailUsecaseOutput{}, err
	}
	detail, err := domain.DetailOf(set, in.ID, from, to, leaks)
	if err != nil {
		return GetGoroutineDetailUsecaseOutput{}, err
	}
	return GetGoroutineDetailUsecaseOutput{Detail: detail}, nil
}

// defaultRange は期間を指定されなかったときの期間を、直近 1 分の Observation から決める。そこにいなければ、セッション全体から探す
func (u *GetGoroutineDetailUsecase) defaultRange(id domain.GoroutineID) (time.Duration, time.Duration, error) {
	until := u.sessions.Session().Observed.Until
	for _, from := range []time.Duration{max(0, until-time.Minute), 0} {
		set, err := observationSet(u.observations, u.sessions, from, until)
		if err != nil {
			return 0, 0, err
		}
		if _, ok := set.Goroutine(id); ok || from == 0 {
			f, t := domain.DefaultDetailRange(set, id)
			return f, t, nil
		}
	}
	return 0, until, nil
}

func (u *GetGoroutineDetailUsecase) leaks() []domain.LeakStack {
	var out []domain.LeakStack
	u.issues.ViewIssueBook(func(b *domain.IssueBook) {
		for _, is := range b.Issues() {
			if is.Kind != domain.IssueGoroutineLeak {
				continue
			}
			o := is.Occurrences[len(is.Occurrences)-1]
			out = append(out, o.Leaks...)
		}
	})
	return out
}
