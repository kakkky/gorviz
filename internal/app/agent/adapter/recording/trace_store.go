package recording

import (
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// TraceStore は取った区切りを server に送るまで古い順に持つ。server とつながらない間に大きさの合計が上限を超えたら、古いものから捨てる (最新の 1 つは残す)
type TraceStore struct {
	mu       sync.Mutex
	chunks   []model.TraceChunk
	bytes    int64
	maxBytes int64
}

func NewTraceStore(maxBytes int64) *TraceStore {
	return &TraceStore{maxBytes: maxBytes}
}

// Append は区切りを足す。runtime は区切りより前から続く待ちの理由を書かないので、直前の区切りから引き継いでから持つ
func (s *TraceStore) Append(chunk model.TraceChunk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var prev model.TraceChunk
	if n := len(s.chunks); n > 0 {
		prev = s.chunks[n-1]
	}
	chunk.InheritWaitReasons(prev)
	s.chunks = append(s.chunks, chunk)
	s.bytes += chunk.SizeBytes()
	for len(s.chunks) > 1 && s.bytes > s.maxBytes {
		s.bytes -= s.chunks[0].SizeBytes()
		s.chunks[0] = model.TraceChunk{}
		s.chunks = s.chunks[1:]
	}
}

// Since は after より後に始まった区切りを、古い順に limit 個まで返す
func (s *TraceStore) Since(after time.Time, limit int) []model.TraceChunk {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.TraceChunk
	for _, c := range s.chunks {
		if !c.StartedAt.After(after) {
			continue
		}
		out = append(out, c)
		if len(out) >= limit {
			break
		}
	}
	return out
}
