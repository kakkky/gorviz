package worker

import (
	"context"
	"log/slog"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// RunAppWorker は server が起動したときに 1 回だけ RunAppUsecase を呼び、アプリが終わるか server が止まるまで待つ
type RunAppWorker struct {
	cfg *config.Config
	run *usecase.RunAppUsecase
}

func NewRunAppWorker(cfg *config.Config, run *usecase.RunAppUsecase) *RunAppWorker {
	return &RunAppWorker{cfg: cfg, run: run}
}

func (w *RunAppWorker) Run(ctx context.Context) error {
	in := usecase.RunAppUsecaseInput{Dir: w.cfg.AppDir, Package: w.cfg.AppPackage, Args: w.cfg.AppArgs, Env: w.cfg.AppEnv(), ShutdownTimeout: w.cfg.ShutdownTimeout}
	if _, err := w.run.Exec(ctx, in); err != nil {
		slog.ErrorContext(ctx, "run the app", slog.Any("err", err))
	}
	return nil
}
