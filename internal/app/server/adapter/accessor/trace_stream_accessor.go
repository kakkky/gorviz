package accessor

import (
	"io"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/tracestream"
)

type TraceStreamAccessor struct {
	stream *tracestream.TraceStream
}

func NewTraceStreamAccessor(s *tracestream.TraceStream) *TraceStreamAccessor {
	return &TraceStreamAccessor{stream: s}
}

func (a *TraceStreamAccessor) AppendTrace(p []byte) error {
	if _, err := a.stream.Write(p); err != nil {
		return domain.ErrInternal.Wrap(err, "append execution trace")
	}
	return nil
}

func (a *TraceStreamAccessor) CloseTrace(broken bool) { a.stream.Close(broken) }

func (a *TraceStreamAccessor) TraceReader() io.Reader { return a.stream }

func (a *TraceStreamAccessor) TraceBroken() bool { return a.stream.Broken() }
