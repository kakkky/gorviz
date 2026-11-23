package usecase

import (
	"context"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type ExportTodosCSVUsecaseInput struct{}

type ExportTodosCSVUsecaseOutput struct {
	CSV string
}

type ExportTodosCSVUsecase struct {
	todos domain.TodoAccessor
}

func (us *UseCase) NewExportTodosCSVUsecase() *ExportTodosCSVUsecase {
	return &ExportTodosCSVUsecase{todos: us.todos}
}

func (u *ExportTodosCSVUsecase) Exec(ctx context.Context, in ExportTodosCSVUsecaseInput) (ExportTodosCSVUsecaseOutput, error) {
	todos, err := u.todos.List(ctx)
	if err != nil {
		return ExportTodosCSVUsecaseOutput{}, err
	}
	domain.SortForList(todos)
	return ExportTodosCSVUsecaseOutput{CSV: domain.ExportCSV(todos)}, nil
}
