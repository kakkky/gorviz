package usecase

import "github.com/kakkky/gorviz/example/todo-app/domain"

type UseCase struct {
	todos    domain.TodoAccessor
	backups  domain.BackupFileAccessor
	notifier domain.NotifierClient
	mail     domain.MailClient
}

func NewUseCase(todos domain.TodoAccessor, backups domain.BackupFileAccessor, notifier domain.NotifierClient, mail domain.MailClient) *UseCase {
	return &UseCase{todos: todos, backups: backups, notifier: notifier, mail: mail}
}
