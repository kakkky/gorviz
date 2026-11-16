package usecase

import (
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// Settings は server の usecase が使う長さと判定の閾値
type Settings struct {
	TraceWindow time.Duration
	HistorySize int
	Issue       domain.IssuePolicy
	Leak        domain.LeakDetectionPolicy
}

// UseCase は server の側。agent 1 つ分の診断・保存・画面を受け持つ。agent から読む口 (DumpSource・LogSource・agent の TraceRecording) を持つ
type UseCase struct {
	settings       Settings
	dumpSource     domain.DumpSource
	logSource      domain.LogSource
	inMemoryKVS    domain.InMemoryKVS
	traceRecording domain.TraceRecording
	logRecording   domain.LogRecording
	recordings     domain.SavedRecordings
	metricStore    domain.MetricStore
	agentInbox     domain.AgentInbox
	snapshotMu     sync.Mutex
}

func NewUseCase(settings Settings, dumpSource domain.DumpSource, logSource domain.LogSource, inMemoryKVS domain.InMemoryKVS, traceRecording domain.TraceRecording, logRecording domain.LogRecording, recordings domain.SavedRecordings, metricStore domain.MetricStore, agentInbox domain.AgentInbox) *UseCase {
	return &UseCase{
		settings:       settings,
		dumpSource:     dumpSource,
		logSource:      logSource,
		inMemoryKVS:    inMemoryKVS,
		traceRecording: traceRecording,
		logRecording:   logRecording,
		recordings:     recordings,
		metricStore:    metricStore,
		agentInbox:     agentInbox,
	}
}
