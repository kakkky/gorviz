package recording

import (
	"sync"
	"time"

	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
)

// TraceStore は録画した区切りを古い順に持つリングバッファ。大きさの合計が上限を超えたら古いものから捨てる (最新の 1 つは残す)。
// 上限は時間ではなくバイト数なので、対象が忙しいほど持てる時間は短くなる
type TraceStore struct {
	mu       sync.RWMutex
	chunks   []domain.TraceChunk
	bytes    int64
	maxBytes int64
}

func NewTraceStore(cfg *config.Config) *TraceStore {
	return &TraceStore{maxBytes: cfg.TraceBufferBytes}
}

func (s *TraceStore) Append(chunk domain.TraceChunk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, chunk)
	s.bytes += chunk.SizeBytes()
	for len(s.chunks) > 1 && s.bytes > s.maxBytes {
		s.bytes -= s.chunks[0].SizeBytes()
		s.chunks[0] = domain.TraceChunk{}
		s.chunks = s.chunks[1:]
	}
}

func (s *TraceStore) Span() (domain.RecordingSpan, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.chunks) == 0 {
		return domain.RecordingSpan{}, false
	}
	return domain.RecordingSpan{From: s.chunks[0].StartedAt, To: s.chunks[len(s.chunks)-1].EndedAt(), Bytes: s.bytes, LimitBytes: s.maxBytes}, true
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

func (s *TraceStore) Between(from, to time.Time) []domain.TraceChunk {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.TraceChunk
	for _, c := range s.chunks {
		if c.EndedAt().Before(from) || c.StartedAt.After(to) {
			continue
		}
		out = append(out, c)
	}
	return out
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
