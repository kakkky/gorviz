package agentrpc

import (
	"github.com/kakkky/gorviz/domain"
)

// Service は agent の側で、server からの呼び出しに答える。取ったものを返すだけで、判定はしない
type Service struct {
	traces domain.TraceRecording
	dumps  domain.DumpSource
	logs   domain.LogSource
	sched  domain.SchedTraceSource
}

func NewService(traces domain.TraceRecording, dumps domain.DumpSource, logs domain.LogSource, sched domain.SchedTraceSource) *Service {
	return &Service{traces: traces, dumps: dumps, logs: logs, sched: sched}
}

func (s *Service) LatestDump(_ Empty, r *DumpReply) error {
	r.Dump, r.OK = s.dumps.LatestDump()
	return nil
}

func (s *Service) LogsSince(a LogsArgs, r *LogsReply) error {
	r.Lines = s.logs.LogsSince(a.After, a.Limit)
	return nil
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

func (s *Service) SchedTrace(_ Empty, r *SchedTraceReply) error {
	r.Report, r.OK = s.sched.Latest()
	return nil
}

func strip(c domain.TraceChunk, withRaw bool) domain.TraceChunk {
	if !withRaw {
		c.Raw = nil
	}
	return c
}
