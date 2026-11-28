package tracestream

import "sync"

// TraceStream はアプリから取った trace の区切りを、取った順に読み手へ渡す。取り手を待たせないよう、読まれるまで溜める。区切りの形は問わない
type TraceStream struct {
	mu     sync.Mutex
	cond   *sync.Cond
	queue  []any
	closed bool
}

func NewTraceStream() *TraceStream {
	s := &TraceStream{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *TraceStream) Append(c any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.queue = append(s.queue, c)
	s.cond.Signal()
	return true
}

// Next は次の区切りが来るまで待つ。閉じて読み切ったら false
func (s *TraceStream) Next() (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.queue) == 0 {
		if s.closed {
			return nil, false
		}
		s.cond.Wait()
	}
	c := s.queue[0]
	s.queue[0] = nil
	s.queue = s.queue[1:]
	return c, true
}

func (s *TraceStream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.cond.Broadcast()
}
