package usecase

import (
	"sync"

	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
)

// UseCase は agent と server のどちらでも使う。agent は対象から取る口 (RuntimeClient・LogParser) を、
// server は agent から読む口 (DumpSource・LogSource・agent の TraceRecording) を持ち、使わない口は nil のままにする
type UseCase struct {
	cfg            *config.Config
	runtimeClient  domain.RuntimeClient
	dumpSource     domain.DumpSource
	logSource      domain.LogSource
	inMemoryKVS    domain.InMemoryKVS
	schedTrace     domain.SchedTraceSource
	traceRecording domain.TraceRecording
	logRecording   domain.LogRecording
	logParser      domain.LogParser
	recordings     domain.SavedRecordings
	snapshotMu     sync.Mutex
}

// NewAgentUseCase は agent の側。対象から trace・dump・ログを取って持つ
func NewAgentUseCase(cfg *config.Config, runtimeClient domain.RuntimeClient, inMemoryKVS domain.InMemoryKVS, schedTrace domain.SchedTraceSource, traceRecording domain.TraceRecording, logRecording domain.LogRecording, logParser domain.LogParser) *UseCase {
	return &UseCase{
		cfg:            cfg,
		runtimeClient:  runtimeClient,
		inMemoryKVS:    inMemoryKVS,
		schedTrace:     schedTrace,
		traceRecording: traceRecording,
		logRecording:   logRecording,
		logParser:      logParser,
	}
}

// NewServerUseCase は server の側。agent 1 つ分の診断・保存・画面を受け持つ
func NewServerUseCase(cfg *config.Config, dumpSource domain.DumpSource, logSource domain.LogSource, inMemoryKVS domain.InMemoryKVS, schedTrace domain.SchedTraceSource, traceRecording domain.TraceRecording, logRecording domain.LogRecording, recordings domain.SavedRecordings) *UseCase {
	return &UseCase{
		cfg:            cfg,
		dumpSource:     dumpSource,
		logSource:      logSource,
		inMemoryKVS:    inMemoryKVS,
		schedTrace:     schedTrace,
		traceRecording: traceRecording,
		logRecording:   logRecording,
		recordings:     recordings,
	}
}
