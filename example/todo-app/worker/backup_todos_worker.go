package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/usecase"
)

type BackupTodosWorker struct {
	every time.Duration
	uc    *usecase.BackupTodosUsecase
}

func NewBackupTodosWorker(every time.Duration, uc *usecase.UseCase) *BackupTodosWorker {
	return &BackupTodosWorker{every: every, uc: uc.NewBackupTodosUsecase()}
}

func (w *BackupTodosWorker) Run(ctx context.Context) error {
	for {
		t := time.NewTimer(w.every)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
		start := time.Now()
		out, err := w.uc.Exec(ctx, usecase.BackupTodosUsecaseInput{})
		if err != nil {
			slog.ErrorContext(ctx, "scheduled backup failed", slog.Any("err", err))
			continue
		}
		slog.InfoContext(ctx, "scheduled backup done", slog.String("path", out.Path), slog.Duration("duration", time.Since(start)))
	}
}
