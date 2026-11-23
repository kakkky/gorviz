// Package usecase は gorviz-server の処理の手順
package usecase

import (
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// Settings は usecase が使う設定の値
type Settings struct {
	Retention     domain.Retention
	MaxDiskBytes  int64
	GUI           domain.GUI
	Notifications map[observation.ServiceName][]domain.Destination
}

type UseCase struct {
	settings    Settings
	locker      domain.InstanceLocker
	holdings    domain.HoldingAccessor
	inventory   domain.InventoryAccessor
	logs        domain.LogAccessor
	traces      domain.TraceAccessor
	snapshots   domain.SnapshotAccessor
	leakChecks  domain.LeakCheckAccessor
	lifecycle   domain.LifecycleAccessor
	metrics     domain.MetricAccessor
	breakdowns  domain.BreakdownAccessor
	issues      domain.IssueAccessor
	preserved   domain.PreservedTraceAccessor
	logWriters  domain.LogWriterAccessor
	checkpoints domain.CheckpointAccessor
	dataFiles   domain.DataFileAccessor
	webhook     domain.NotificationClient
	slack       domain.NotificationClient
	mail        domain.NotificationClient
}

func NewUseCase(
	settings Settings,
	locker domain.InstanceLocker,
	holdings domain.HoldingAccessor,
	inventory domain.InventoryAccessor,
	logs domain.LogAccessor,
	traces domain.TraceAccessor,
	snapshots domain.SnapshotAccessor,
	leakChecks domain.LeakCheckAccessor,
	lifecycle domain.LifecycleAccessor,
	metrics domain.MetricAccessor,
	breakdowns domain.BreakdownAccessor,
	issues domain.IssueAccessor,
	preserved domain.PreservedTraceAccessor,
	logWriters domain.LogWriterAccessor,
	checkpoints domain.CheckpointAccessor,
	dataFiles domain.DataFileAccessor,
	webhook domain.NotificationClient,
	slack domain.NotificationClient,
	mail domain.NotificationClient,
) *UseCase {
	return &UseCase{
		settings: settings, locker: locker, holdings: holdings, inventory: inventory, logs: logs, traces: traces, snapshots: snapshots,
		leakChecks: leakChecks, lifecycle: lifecycle, metrics: metrics, breakdowns: breakdowns, issues: issues, preserved: preserved,
		logWriters: logWriters, checkpoints: checkpoints, dataFiles: dataFiles, webhook: webhook, slack: slack, mail: mail,
	}
}
