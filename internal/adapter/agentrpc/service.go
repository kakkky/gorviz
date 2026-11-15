package agentrpc

import (
	"github.com/kakkky/gorviz/internal/domain"
)

// Service は agent の側で、pull の接続の上で server からの trace の要求に答える。持っているものを返すだけで、判定はしない
type Service struct {
	traces domain.TraceRecording
}

func NewService(traces domain.TraceRecording) *Service {
	return &Service{traces: traces}
}

func (s *Service) LatestChunk(a ChunkArgs, r *ChunkReply) error {
	c, ok := s.traces.Latest()
	if ok && !a.IfNewerThan.IsZero() && !c.StartedAt.After(a.IfNewerThan) {
		r.NotModified = true
		return nil
	}
	r.Chunk, r.OK = strip(c, a.WithRaw), ok
	return nil
}

func (s *Service) ChunkAt(a ChunkArgs, r *ChunkReply) error {
	c, ok := s.traces.At(a.At)
	r.Chunk, r.OK = strip(c, a.WithRaw), ok
	return nil
}

func (s *Service) ChunksBetween(a BetweenArgs, r *ChunksReply) error {
	for _, c := range s.traces.Between(a.From, a.To) {
		r.Chunks = append(r.Chunks, strip(c, a.WithRaw))
	}
	return nil
}

func (s *Service) FindLogMark(a SeqArgs, r *MarkReply) error {
	c, m, ok := s.traces.FindLogMark(a.Seq)
	r.Chunk, r.Mark, r.OK = strip(c, false), m, ok
	return nil
}

func (s *Service) Span(_ Empty, r *SpanReply) error {
	r.Span, r.OK = s.traces.Span()
	return nil
}

func strip(c domain.TraceChunk, withRaw bool) domain.TraceChunk {
	if !withRaw {
		c.Raw = nil
	}
	return c
}
