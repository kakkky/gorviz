package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type ComputeTodoStatsUsecaseInput struct{}

type ComputeTodoStatsUsecaseOutput struct {
	Stats domain.Stats
	Took  time.Duration
}

type ComputeTodoStatsUsecase struct {
	todos domain.TodoAccessor
}

func (us *UseCase) NewComputeTodoStatsUsecase() *ComputeTodoStatsUsecase {
	return &ComputeTodoStatsUsecase{todos: us.todos}
}

func (u *ComputeTodoStatsUsecase) Exec(ctx context.Context, in ComputeTodoStatsUsecaseInput) (ComputeTodoStatsUsecaseOutput, error) {
	todos, err := u.todos.List(ctx)
	if err != nil {
		return ComputeTodoStatsUsecaseOutput{}, err
	}
	start := time.Now()
	stats := domain.ComputeStats(todos)
	return ComputeTodoStatsUsecaseOutput{Stats: stats, Took: time.Since(start)}, nil
}
