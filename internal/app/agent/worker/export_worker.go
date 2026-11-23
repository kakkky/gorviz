package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
)

type ExportWorker struct {
	uc       *usecase.ExportObservationsUsecase
	in       usecase.ExportObservationsUsecaseInput
	interval time.Duration
}

func NewExportWorker(cfg *config.Config, uc *usecase.ExportObservationsUsecase) *ExportWorker {
	return &ExportWorker{uc: uc, in: usecase.ExportObservationsUsecaseInput{Agent: cfg.AgentID}, interval: cfg.ExportInterval}
}

func (w *ExportWorker) Run(ctx context.Context) error {
	for {
		out, err := w.uc.Exec(ctx, w.in)
		if err != nil {
			slog.ErrorContext(ctx, "export observations", slog.Any("err", err))
		}
		for _, in := range out.Settled {
			slog.InfoContext(ctx, "sent the rest of an ended instance", slog.String("service", string(in.Service)), slog.String("instance", string(in.Name)))
		}
		if !wait(ctx, w.interval) {
			return nil
		}
	}
}
