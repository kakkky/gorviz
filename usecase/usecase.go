package usecase

import (
	"sync"

	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
)

type UseCase struct {
	cfg            *config.Config
	runtimeClient  domain.RuntimeClient
	inMemoryKVS    domain.InMemoryKVS
	schedTrace     domain.SchedTraceSource
	traceRecording domain.TraceRecording
	logRecording   domain.LogRecording
	logParser      domain.LogParser
	snapshotMu     sync.Mutex
}

func NewUseCase(cfg *config.Config, runtimeClient domain.RuntimeClient, inMemoryKVS domain.InMemoryKVS, schedTrace domain.SchedTraceSource, traceRecording domain.TraceRecording, logRecording domain.LogRecording, logParser domain.LogParser) *UseCase {
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
