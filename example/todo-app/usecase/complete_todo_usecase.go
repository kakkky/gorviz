package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type CompleteTodoUsecaseInput struct {
	ID domain.TodoID
}

type CompleteTodoUsecaseOutput struct {
	Todo domain.Todo
}

type CompleteTodoUsecase struct {
	todos domain.TodoAccessor
	mail  domain.MailClient
}

func (us *UseCase) NewCompleteTodoUsecase() *CompleteTodoUsecase {
	return &CompleteTodoUsecase{todos: us.todos, mail: us.mail}
}

func (u *CompleteTodoUsecase) Exec(ctx context.Context, in CompleteTodoUsecaseInput) (CompleteTodoUsecaseOutput, error) {
	t, err := u.todos.Complete(ctx, in.ID, time.Now())
	if err != nil {
		return CompleteTodoUsecaseOutput{}, err
	}
	if err := u.mail.SendCompleted(ctx, t); err != nil {
		return CompleteTodoUsecaseOutput{}, err
	}
	return CompleteTodoUsecaseOutput{Todo: t}, nil
}
