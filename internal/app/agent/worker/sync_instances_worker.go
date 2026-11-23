package worker

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type SyncInstancesWorker struct {
	uc       *usecase.SyncInstancesUsecase
	in       usecase.SyncInstancesUsecaseInput
	interval time.Duration
}

// NewSyncInstancesWorker はインスタンスの名前が設定になければ、ホスト名を使う
func NewSyncInstancesWorker(cfg *config.Config, uc *usecase.SyncInstancesUsecase) (*SyncInstancesWorker, error) {
	mode, err := domain.NewMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	name := cfg.Instance
	if name == "" {
		if name, err = os.Hostname(); err != nil {
			return nil, err
		}
	}
	self := domain.SelfTarget(mode, observation.ServiceName(cfg.Service), observation.InstanceName(name), cfg.PprofAddr, cfg.AgentID)
	return &SyncInstancesWorker{uc: uc, in: usecase.SyncInstancesUsecaseInput{Mode: mode, Self: self, PodLogDir: cfg.PodLogDir}, interval: cfg.SyncInterval}, nil
}

func (w *SyncInstancesWorker) Run(ctx context.Context) error {
	for {
		out, err := w.uc.Exec(ctx, w.in)
		if err != nil {
			slog.ErrorContext(ctx, "sync instances", slog.Any("err", err))
		}
		for _, e := range out.Ended {
			slog.InfoContext(ctx, "instance ended", slog.String("service", string(e.Service)), slog.String("instance", string(e.Name)))
		}
		if !wait(ctx, w.interval) {
			return nil
		}
	}
}

func wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
