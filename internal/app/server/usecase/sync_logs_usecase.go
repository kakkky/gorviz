package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const (
	lastSyncedLogKey = "logs:last-synced"
	logSyncBatch     = 5_000
)

// SyncLogsUsecase は server の側で、agent から送られてきたログのうち、まだ読んでいない分を server のログに移して持つ
type SyncLogsUsecase struct {
	logSource    domain.LogSource
	logRecording domain.LogRecording
	inMemoryKVS  domain.InMemoryKVS
}

func (us *UseCase) NewSyncLogsUsecase() *SyncLogsUsecase {
	return &SyncLogsUsecase{logSource: us.logSource, logRecording: us.logRecording, inMemoryKVS: us.inMemoryKVS}
}

type SyncLogsUsecaseInput struct{}

type SyncLogsUsecaseOutput struct {
	Synced int
}

func (us *SyncLogsUsecase) Exec(_ context.Context, _ SyncLogsUsecaseInput) (SyncLogsUsecaseOutput, error) {
	last := domain.KVSGetOr(us.inMemoryKVS, lastSyncedLogKey, model.LogLineID(0))
	lines := us.logSource.LogsSince(last, logSyncBatch)
	for _, l := range lines {
		last = l.ID
		us.logRecording.Append(l)
	}
	us.inMemoryKVS.Set(lastSyncedLogKey, last)
	return SyncLogsUsecaseOutput{Synced: len(lines)}, nil
}
