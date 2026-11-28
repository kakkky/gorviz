package accessor

import (
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/tracestream"
)

type TraceStreamAccessor struct {
	stream *tracestream.TraceStream
}

func NewTraceStreamAccessor(s *tracestream.TraceStream) *TraceStreamAccessor {
	return &TraceStreamAccessor{stream: s}
}

func (a *TraceStreamAccessor) AppendTrace(c domain.TraceChunk) error {
	if !a.stream.Append(c) {
		return domain.ErrInternal.With("append execution trace to a closed stream")
	}
	return nil
}

func (a *TraceStreamAccessor) CloseTrace() { a.stream.Close() }

func (a *TraceStreamAccessor) NextTrace() (domain.TraceChunk, bool) {
	v, ok := a.stream.Next()
	if !ok {
		return domain.TraceChunk{}, false
	}
	return v.(domain.TraceChunk), true
}
