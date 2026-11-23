package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type AddTodoUsecaseInput struct {
	Title    string
	Reminder bool
}

type AddTodoUsecaseOutput struct {
	Todo               domain.Todo
	ReminderRegistered bool
}

type AddTodoUsecase struct {
	todos    domain.TodoAccessor
	notifier domain.NotifierClient
}

func (us *UseCase) NewAddTodoUsecase() *AddTodoUsecase {
	return &AddTodoUsecase{todos: us.todos, notifier: us.notifier}
}

// Exec はリマインダーを登録できなくても、todo は追加したまま返す
func (u *AddTodoUsecase) Exec(ctx context.Context, in AddTodoUsecaseInput) (AddTodoUsecaseOutput, error) {
	title, err := domain.NewTitle(in.Title)
	if err != nil {
		return AddTodoUsecaseOutput{}, err
	}
	t, err := u.todos.Add(ctx, title, in.Reminder, time.Now())
	if err != nil {
		return AddTodoUsecaseOutput{}, err
	}
	if !t.Reminder {
		return AddTodoUsecaseOutput{Todo: t}, nil
	}
	if err := u.notifier.RegisterReminder(ctx, t); err != nil {
		slog.WarnContext(ctx, "reminder not registered", slog.Int("todo", int(t.ID)), slog.Any("err", err))
		return AddTodoUsecaseOutput{Todo: t}, nil
	}
	return AddTodoUsecaseOutput{Todo: t, ReminderRegistered: true}, nil
}
