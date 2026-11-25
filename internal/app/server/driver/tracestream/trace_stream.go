package tracestream

import (
	"io"
	"sync"
)

// TraceStream は agent から届いた execution trace を、届いた順に読み手へ渡す。書き手を待たせないよう、読まれるまで溜める
type TraceStream struct {
	mu     sync.Mutex
	cond   *sync.Cond
	queue  [][]byte
	closed bool
	broken bool
}

func NewTraceStream() *TraceStream {
	s := &TraceStream{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *TraceStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	s.queue = append(s.queue, append([]byte(nil), p...))
	s.cond.Signal()
	return len(p), nil
}

func (s *TraceStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.queue) == 0 {
		if s.closed {
			if s.broken {
				return 0, io.ErrUnexpectedEOF
			}
			return 0, io.EOF
		}
		s.cond.Wait()
	}
	n := copy(p, s.queue[0])
	if n == len(s.queue[0]) {
		s.queue[0] = nil
		s.queue = s.queue[1:]
	} else {
		s.queue[0] = s.queue[0][n:]
	}
	return n, nil
}

// Close は trace の終わり。broken は終わりの合図を受けずに切れたこと
func (s *TraceStream) Close(broken bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed, s.broken = true, broken
	s.cond.Broadcast()
}

func (s *TraceStream) Broken() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.broken
}
