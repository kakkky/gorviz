package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type SeedTodosUsecaseInput struct {
	N int
}

type SeedTodosUsecaseOutput struct{}

type SeedTodosUsecase struct {
	todos domain.TodoAccessor
}

func (us *UseCase) NewSeedTodosUsecase() *SeedTodosUsecase {
	return &SeedTodosUsecase{todos: us.todos}
}

func (u *SeedTodosUsecase) Exec(ctx context.Context, in SeedTodosUsecaseInput) (SeedTodosUsecaseOutput, error) {
	n, err := u.todos.Count(ctx)
	if err != nil {
		return SeedTodosUsecaseOutput{}, err
	}
	now := time.Now()
	for i := range in.N {
		if _, err := u.todos.Add(ctx, domain.SampleTitle(n+i), false, now); err != nil {
			return SeedTodosUsecaseOutput{}, err
		}
	}
	return SeedTodosUsecaseOutput{}, nil
}
