package usecase_test

import (
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// fakeTraces は録画を古い順に持つだけの TraceRecording。本番の server は agent から引くか保存したものを読むので、テストではこれで代える
type fakeTraces struct{ chunks []model.TraceChunk }

func (f *fakeTraces) Append(c model.TraceChunk) { f.chunks = append(f.chunks, c) }

func (f *fakeTraces) Latest() (model.TraceChunk, bool) {
	if len(f.chunks) == 0 {
		return model.TraceChunk{}, false
	}
	return f.chunks[len(f.chunks)-1], true
}

func (f *fakeTraces) At(t time.Time) (model.TraceChunk, bool) {
	for i := len(f.chunks) - 1; i >= 0; i-- {
		if f.chunks[i].Contains(t) {
			return f.chunks[i], true
		}
	}
	return model.TraceChunk{}, false
}

func (f *fakeTraces) Between(from, to time.Time) []model.TraceChunk {
	var out []model.TraceChunk
	for _, c := range f.chunks {
		if !c.EndedAt().Before(from) && !c.StartedAt.After(to) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeTraces) FindLogMark(seq uint64) (model.TraceChunk, model.TraceLogMark, bool) {
	for i := len(f.chunks) - 1; i >= 0; i-- {
		for _, m := range f.chunks[i].LogMarks {
			if m.Seq == seq {
				return f.chunks[i], m, true
			}
		}
	}
	return model.TraceChunk{}, model.TraceLogMark{}, false
}

func (f *fakeTraces) Span() (model.RecordingSpan, bool) {
	if len(f.chunks) == 0 {
		return model.RecordingSpan{}, false
	}
	return model.RecordingSpan{From: f.chunks[0].StartedAt, To: f.chunks[len(f.chunks)-1].EndedAt()}, true
}
