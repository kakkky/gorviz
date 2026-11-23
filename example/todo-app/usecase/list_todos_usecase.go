package usecase

import (
	"context"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

const maxListedTodos = 200

type ListTodosUsecaseInput struct {
	Query string
}

type ListTodosUsecaseOutput struct {
	Todos []domain.Todo
	Total int
}

type ListTodosUsecase struct {
	todos domain.TodoAccessor
}

func (us *UseCase) NewListTodosUsecase() *ListTodosUsecase {
	return &ListTodosUsecase{todos: us.todos}
}

func (u *ListTodosUsecase) Exec(ctx context.Context, in ListTodosUsecaseInput) (ListTodosUsecaseOutput, error) {
	all, err := u.todos.List(ctx)
	if err != nil {
		return ListTodosUsecaseOutput{}, err
	}
	matched := make([]domain.Todo, 0, len(all))
	for _, t := range all {
		if t.Matches(in.Query) {
			matched = append(matched, t)
		}
	}
	domain.SortForList(matched)
	return ListTodosUsecaseOutput{Todos: matched[:min(len(matched), maxListedTodos)], Total: len(all)}, nil
}
