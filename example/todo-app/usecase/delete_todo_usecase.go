package usecase

import (
	"context"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type DeleteTodoUsecaseInput struct {
	ID domain.TodoID
}

type DeleteTodoUsecaseOutput struct{}

type DeleteTodoUsecase struct {
	todos domain.TodoAccessor
}

func (us *UseCase) NewDeleteTodoUsecase() *DeleteTodoUsecase {
	return &DeleteTodoUsecase{todos: us.todos}
}

func (u *DeleteTodoUsecase) Exec(ctx context.Context, in DeleteTodoUsecaseInput) (DeleteTodoUsecaseOutput, error) {
	if err := u.todos.Delete(ctx, in.ID); err != nil {
		return DeleteTodoUsecaseOutput{}, err
	}
	return DeleteTodoUsecaseOutput{}, nil
}
