package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type GetLastBackupUsecaseInput struct{}

type GetLastBackupUsecaseOutput struct {
	At  time.Time
	Has bool
}

type GetLastBackupUsecase struct {
	backups domain.BackupFileAccessor
}

func (us *UseCase) NewGetLastBackupUsecase() *GetLastBackupUsecase {
	return &GetLastBackupUsecase{backups: us.backups}
}

func (u *GetLastBackupUsecase) Exec(ctx context.Context, in GetLastBackupUsecaseInput) (GetLastBackupUsecaseOutput, error) {
	at, ok := u.backups.LastWrittenAt(ctx)
	return GetLastBackupUsecaseOutput{At: at, Has: ok}, nil
}
