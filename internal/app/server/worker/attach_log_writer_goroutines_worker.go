package worker

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

type AttachLogWriterGoroutinesWorker struct {
	uc       *usecase.AttachLogWriterGoroutinesUsecase
	interval time.Duration
}

func NewAttachLogWriterGoroutinesWorker(cfg *config.Config, uc *usecase.AttachLogWriterGoroutinesUsecase) *AttachLogWriterGoroutinesWorker {
	return &AttachLogWriterGoroutinesWorker{uc: uc, interval: cfg.AttachLogWriterGoroutinesInterval}
}

func (w *AttachLogWriterGoroutinesWorker) Run(ctx context.Context) error {
	return loop(ctx, "attach_log_writer_goroutines", w.interval, func(ctx context.Context, now time.Time) error {
		_, err := w.uc.Exec(ctx, usecase.AttachLogWriterGoroutinesUsecaseInput{Now: now})
		return err
	})
}
