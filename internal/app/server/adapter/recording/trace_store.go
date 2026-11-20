package recording

import (
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// TraceStore は agent から届いた区切りを古い順に持つリングバッファ。大きさの合計が上限を超えたら古いものから捨てる (最新の 1 つは残す)。
// 上限は時間ではなくバイト数なので、対象が忙しいほど持てる時間は短くなる
type TraceStore struct {
	mu       sync.RWMutex
	chunks   []model.TraceChunk
	bytes    int64
	maxBytes int64
}

func NewTraceStore(maxBytes int64) *TraceStore {
	return &TraceStore{maxBytes: maxBytes}
}

// Append は区切りを足す。agent はつなぎ直すと手元の区切りを最初から送り直すので、持っている最新より古いものは捨てる
func (s *TraceStore) Append(chunk model.TraceChunk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := len(s.chunks); n > 0 && !chunk.StartedAt.After(s.chunks[n-1].StartedAt) {
		return
	}
	s.chunks = append(s.chunks, chunk)
	s.bytes += chunk.SizeBytes()
	for len(s.chunks) > 1 && s.bytes > s.maxBytes {
		s.bytes -= s.chunks[0].SizeBytes()
		s.chunks[0] = model.TraceChunk{}
		s.chunks = s.chunks[1:]
	}
}

func (s *TraceStore) Span() (model.RecordingSpan, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.chunks) == 0 {
		return model.RecordingSpan{}, false
	}
	return model.RecordingSpan{From: s.chunks[0].StartedAt, To: s.chunks[len(s.chunks)-1].EndedAt(), Bytes: s.bytes, LimitBytes: s.maxBytes}, true
}

func (s *TraceStore) Latest() (model.TraceChunk, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.chunks) == 0 {
		return model.TraceChunk{}, false
	}
	return s.chunks[len(s.chunks)-1], true
}

func (s *TraceStore) At(t time.Time) (model.TraceChunk, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.chunks) - 1; i >= 0; i-- {
		if s.chunks[i].Contains(t) {
			return s.chunks[i], true
		}
	}
	return model.TraceChunk{}, false
}

func (s *TraceStore) Between(from, to time.Time) []model.TraceChunk {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []model.TraceChunk
	for _, c := range s.chunks {
		if c.EndedAt().Before(from) || c.StartedAt.After(to) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (s *TraceStore) FindLogMark(seq uint64) (model.TraceChunk, model.TraceLogMark, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.chunks) - 1; i >= 0; i-- {
		for _, m := range s.chunks[i].LogMarks {
			if m.Seq == seq {
				return s.chunks[i], m, true
			}
		}
	}
	return model.TraceChunk{}, model.TraceLogMark{}, false
}
