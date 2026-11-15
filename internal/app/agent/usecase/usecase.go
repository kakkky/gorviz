package usecase

import (
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// Settings は agent の usecase が使う間隔
type Settings struct {
	ChunkDuration  time.Duration
	LeakCheckEvery int
}

// UseCase は agent の側。対象から trace・dump・ログを取って持つ。判定はしない
type UseCase struct {
	settings       Settings
	runtimeClient  domain.RuntimeClient
	inMemoryKVS    domain.InMemoryKVS
	traceRecording domain.TraceRecording
	logRecording   domain.LogRecording
	logParser      domain.LogParser
}

func NewUseCase(settings Settings, runtimeClient domain.RuntimeClient, inMemoryKVS domain.InMemoryKVS, traceRecording domain.TraceRecording, logRecording domain.LogRecording, logParser domain.LogParser) *UseCase {
	return &UseCase{
		settings:       settings,
		runtimeClient:  runtimeClient,
		inMemoryKVS:    inMemoryKVS,
		traceRecording: traceRecording,
		logRecording:   logRecording,
		logParser:      logParser,
	}
}
