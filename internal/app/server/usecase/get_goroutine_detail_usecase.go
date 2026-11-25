package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// GetGoroutineDetailUsecase は 1 つの goroutine が、期間の中で何に止まっていたかを返す。期間がなければ goroutine がいた期間 (直近 1 分まで)
type GetGoroutineDetailUsecase struct {
	index  domain.TraceIndexStore
	issues domain.IssueBookStore
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
	return &GetGoroutineDetailUsecase{index: us.index, issues: us.issues}
}

func (u *GetGoroutineDetailUsecase) Exec(_ context.Context, in GetGoroutineDetailUsecaseInput) (GetGoroutineDetailUsecaseOutput, error) {
	if in.HasRange && in.To < in.From {
		return GetGoroutineDetailUsecaseOutput{}, domain.ErrInvalidArgument.With("the end of the range is before its start")
	}
	leaks := u.leaks()
	var out GetGoroutineDetailUsecaseOutput
	var err error
	u.index.ViewTraceIndex(func(ix *domain.TraceIndex) {
		from, to := in.From, in.To
		if !in.HasRange {
			from, to = domain.DefaultDetailRange(ix, in.ID)
		}
		out.Detail, err = domain.DetailOf(ix, in.ID, from, to, leaks)
	})
	return out, err
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
