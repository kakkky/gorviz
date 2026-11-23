package accessor

import (
	"context"
	"io"
	"path"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
)

// indexEntry は execution trace の索引の 1 項目。チャンク 1 つの時刻と、.pb.gz の中の位置
type indexEntry struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Offset int64     `json:"offset"`
	Length int64     `json:"length"`
}

type TraceAccessor struct{ root *filesystem.Root }

func NewTraceAccessor(root *filesystem.Root) *TraceAccessor { return &TraceAccessor{root: root} }

func traceFiles(s observation.ServiceName, i observation.InstanceName, t time.Time) (string, string) {
	return instanceFile(s, i, domain.DataExecutionTraces, t, ".pb.gz"), instanceFile(s, i, domain.DataExecutionTraces, t, ".index")
}

// validTrace は索引にないチャンクを切り詰める
func (a *TraceAccessor) validTrace(index string) filesystem.ValidLength {
	return func(io.ReaderAt, int64) (int64, error) {
		entries, err := readLines[indexEntry](a.root, index)
		if err != nil {
			return 0, err
		}
		var end int64
		for _, e := range entries {
			end = max(end, e.Offset+e.Length)
		}
		return end, nil
	}
}

func (a *TraceAccessor) AppendTraceChunks(ctx context.Context, instance observation.Instance, chunks []observation.TraceChunk) error {
	for _, c := range chunks {
		data, index := traceFiles(instance.Service, instance.Name, c.StartedAt)
		b, err := gzipMember(agentrpc.ToTraceChunk(c))
		if err != nil {
			return domain.ErrInternal.Wrap(err, "encode a trace chunk")
		}
		off, err := a.root.Append(data, b, a.validTrace(index))
		if err != nil {
			return domain.ErrInternal.Wrap(err, "append a trace chunk")
		}
		line, err := checkedLine(indexEntry{Start: c.StartedAt, End: c.EndedAt(), Offset: off, Length: int64(len(b))})
		if err != nil {
			return domain.ErrInternal.Wrap(err, "encode a trace index entry")
		}
		if _, err := a.root.Append(index, line, validLines); err != nil {
			return domain.ErrInternal.Wrap(err, "append a trace index entry")
		}
	}
	return nil
}

func (a *TraceAccessor) entries(s observation.ServiceName, i observation.InstanceName, from, to time.Time) ([]struct {
	file string
	e    indexEntry
}, error) {
	var out []struct {
		file string
		e    indexEntry
	}
	for _, p := range periods(domain.DataExecutionTraces, from, to, 1) {
		data, index := traceFiles(s, i, p)
		entries, err := readLines[indexEntry](a.root, index)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Start.Before(to) && e.End.After(from) {
				out = append(out, struct {
					file string
					e    indexEntry
				}{data, e})
			}
		}
	}
	return out, nil
}

func (a *TraceAccessor) ListTraceChunks(ctx context.Context, s observation.ServiceName, i observation.InstanceName, from, to time.Time) ([]observation.TraceChunk, error) {
	entries, err := a.entries(s, i, from, to)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read the trace index")
	}
	var out []observation.TraceChunk
	for _, x := range entries {
		if err := ctx.Err(); err != nil {
			return nil, domain.ErrInternal.Wrap(err, "list trace chunks")
		}
		b, err := a.root.ReadAt(x.file, x.e.Offset, x.e.Length)
		if err != nil {
			return nil, domain.ErrInternal.Wrap(err, "read a trace chunk")
		}
		_, err = eachMember(b, func(raw []byte, _ int64) error {
			var m agentrpc.TraceChunk
			if err := proto.Unmarshal(raw, &m); err != nil {
				return err
			}
			out = append(out, agentrpc.FromTraceChunk(&m))
			return nil
		})
		if err != nil {
			warnBroken(x.file, err)
		}
	}
	return out, nil
}

func (a *TraceAccessor) ListTraceRanges(ctx context.Context, s observation.ServiceName, i observation.InstanceName, from, to time.Time) ([]domain.TimeRange, error) {
	entries, err := a.entries(s, i, from, to)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read the trace index")
	}
	var out []domain.TimeRange
	for _, x := range entries {
		// チャンクの間がほぼ空いていなければ 1 つの範囲にまとめる
		if n := len(out); n > 0 && !x.e.Start.After(out[n-1].To.Add(time.Second)) {
			out[n-1].To = maxTime(out[n-1].To, x.e.End)
			continue
		}
		out = append(out, domain.TimeRange{From: x.e.Start, To: x.e.End})
	}
	return out, nil
}

// lastEntry は最新の索引の最後の項目
func (a *TraceAccessor) lastEntry(s observation.ServiceName, i observation.InstanceName) (indexEntry, bool, error) {
	dir := path.Join(instanceDir(s, i), string(domain.DataExecutionTraces))
	entries, err := a.root.ReadDir(dir)
	if err != nil {
		return indexEntry{}, false, err
	}
	for j := len(entries) - 1; j >= 0; j-- {
		if !strings.HasSuffix(entries[j].Name(), ".index") {
			continue
		}
		lines, err := readLines[indexEntry](a.root, path.Join(dir, entries[j].Name()))
		if err != nil {
			return indexEntry{}, false, err
		}
		if n := len(lines); n > 0 {
			return lines[n-1], true, nil
		}
	}
	return indexEntry{}, false, nil
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
