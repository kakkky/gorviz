package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/domain"
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
	last := getOr(us.inMemoryKVS, lastSyncedLogKey, domain.LogLineID(0))
	lines := us.logSource.LogsSince(last, logSyncBatch)
	for _, l := range lines {
		last = l.ID
		us.logRecording.Append(l)
	}
	us.inMemoryKVS.Set(lastSyncedLogKey, last)
	return SyncLogsUsecaseOutput{Synced: len(lines)}, nil
}

// LogsSince は domain.LogSource として、agent の側で取り込んだログのうち after より新しいものを古い順に返す
func (us *UseCase) LogsSince(after domain.LogLineID, limit int) []domain.LogLine {
	var out []domain.LogLine
	for _, l := range us.logRecording.Snapshot() {
		if l.ID <= after {
			continue
		}
		out = append(out, l)
		if len(out) >= limit {
			break
		}
	}
	return out
}
