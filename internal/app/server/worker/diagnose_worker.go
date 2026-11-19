// Package worker は server の側で、同じプロセスの中で時間ごとに回り続ける入口。外から呼ばれるのではなく、自分で usecase を呼び続ける
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// DiagnoseWorker は agent 1 つ分の判定を回し続ける。
// refresh ごとに、agent から送られてきた軽いもの (ログ・dump) で判定と集計をし、閉じた区切りの集計と、見つかった Issue と、その前後の録画を保存する
type DiagnoseWorker struct {
	agent                        string
	connected                    func() bool
	refresh                      time.Duration
	syncLogsUsecase              *usecase.SyncLogsUsecase
	recordRuntimeSnapshotUsecase *usecase.RecordRuntimeSnapshotUsecase
	saveRecordingsUsecase        *usecase.SaveRecordingsUsecase
	saveMetricsUsecase           *usecase.SaveMetricsUsecase
	recordIssuesUsecase          *usecase.RecordIssuesUsecase
}

func NewDiagnoseWorker(agent string, connected func() bool, refresh time.Duration, uc *usecase.UseCase) *DiagnoseWorker {
	return &DiagnoseWorker{
		agent:                        agent,
		connected:                    connected,
		refresh:                      refresh,
		syncLogsUsecase:              uc.NewSyncLogsUsecase(),
		recordRuntimeSnapshotUsecase: uc.NewRecordRuntimeSnapshotUsecase(),
		saveRecordingsUsecase:        uc.NewSaveRecordingsUsecase(),
		saveMetricsUsecase:           uc.NewSaveMetricsUsecase(),
		recordIssuesUsecase:          uc.NewRecordIssuesUsecase(),
	}
}

func (d *DiagnoseWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(d.refresh)
	defer ticker.Stop()
	for {
		if d.connected() {
			d.diagnose(ctx, time.Now())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (d *DiagnoseWorker) diagnose(ctx context.Context, now time.Time) {
	if _, err := d.syncLogsUsecase.Exec(ctx, usecase.SyncLogsUsecaseInput{}); err != nil {
		slog.WarnContext(ctx, "sync logs", slog.String("agent", d.agent), slog.Any("err", err))
	}
	if _, err := d.recordRuntimeSnapshotUsecase.Exec(ctx, usecase.RecordRuntimeSnapshotUsecaseInput{Now: now}); err != nil && !errors.Is(err, model.ErrNotFound) {
		slog.WarnContext(ctx, "record runtime snapshot", slog.String("agent", d.agent), slog.Any("err", err))
	}
	if _, err := d.saveMetricsUsecase.Exec(ctx, usecase.SaveMetricsUsecaseInput{Now: now}); err != nil {
		slog.WarnContext(ctx, "save metrics", slog.String("agent", d.agent), slog.Any("err", err))
	}
	if _, err := d.recordIssuesUsecase.Exec(ctx, usecase.RecordIssuesUsecaseInput{Now: now}); err != nil {
		slog.WarnContext(ctx, "record issues", slog.String("agent", d.agent), slog.Any("err", err))
	}
	out, err := d.saveRecordingsUsecase.Exec(ctx, usecase.SaveRecordingsUsecaseInput{Now: now})
	if err != nil {
		slog.WarnContext(ctx, "save recordings", slog.String("agent", d.agent), slog.Any("err", err))
		return
	}
	for _, r := range out.Saved {
		slog.InfoContext(ctx, "saved a recording", slog.String("agent", d.agent), slog.String("id", r.ID), slog.Int("chunks", r.Chunks))
	}
}
