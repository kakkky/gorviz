package usecase

import (
	"context"
	"encoding/json"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type BackupTodosUsecaseInput struct{}

type BackupTodosUsecaseOutput struct {
	Path string
}

type BackupTodosUsecase struct {
	todos   domain.TodoAccessor
	backups domain.BackupFileAccessor
}

func (us *UseCase) NewBackupTodosUsecase() *BackupTodosUsecase {
	return &BackupTodosUsecase{todos: us.todos, backups: us.backups}
}

// Exec の不具合 (LOCK_CONTENTION): 書き出しが終わるまで todo の置き場のロックを握ったままなので、その間のリクエストがすべてロック待ちになる
func (u *BackupTodosUsecase) Exec(ctx context.Context, in BackupTodosUsecaseInput) (BackupTodosUsecaseOutput, error) {
	var path string
	err := u.todos.WithAllLocked(ctx, func(todos []domain.Todo) error {
		data, err := json.Marshal(todos)
		if err != nil {
			return domain.ErrInternal.Wrap(err, "encode backup")
		}
		path, err = u.backups.Write(ctx, data)
		return err
	})
	if err != nil {
		return BackupTodosUsecaseOutput{}, err
	}
	return BackupTodosUsecaseOutput{Path: path}, nil
}
