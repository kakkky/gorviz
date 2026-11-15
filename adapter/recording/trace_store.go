package recording

import (
	"sync"
	"time"

	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
)

// TraceStore は録画した区切りを古い順に持ち、保持期間かイベント数の上限を超えたら古いものから捨てる。
type TraceStore struct {
	mu        sync.RWMutex
	chunks    []domain.TraceChunk
	events    int
	retention time.Duration
	maxEvents int
}

func NewTraceStore(cfg *config.Config) *TraceStore {
	return &TraceStore{retention: cfg.Retention, maxEvents: cfg.MaxRecordedEvents}
}

func (s *TraceStore) Append(chunk domain.TraceChunk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, chunk)
	s.events += len(chunk.Timeline.Events)
	cutoff := chunk.EndedAt().Add(-s.retention)
	for len(s.chunks) > 1 && (s.chunks[0].EndedAt().Before(cutoff) || s.events > s.maxEvents) {
		s.events -= len(s.chunks[0].Timeline.Events)
		s.chunks[0] = domain.TraceChunk{}
		s.chunks = s.chunks[1:]
	}
}

func (s *TraceStore) Latest() (domain.TraceChunk, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.chunks) == 0 {
		return domain.TraceChunk{}, false
	}
	return s.chunks[len(s.chunks)-1], true
}

func (s *TraceStore) At(t time.Time) (domain.TraceChunk, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.chunks) - 1; i >= 0; i-- {
		if s.chunks[i].Contains(t) {
			return s.chunks[i], true
		}
	}
	return domain.TraceChunk{}, false
}

func (s *TraceStore) FindLogMark(seq uint64) (domain.TraceChunk, domain.TraceLogMark, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.chunks) - 1; i >= 0; i-- {
		for _, m := range s.chunks[i].LogMarks {
			if m.Seq == seq {
				return s.chunks[i], m, true
			}
		}
	}
	return domain.TraceChunk{}, domain.TraceLogMark{}, false
}
