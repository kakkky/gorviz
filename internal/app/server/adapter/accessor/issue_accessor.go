package accessor

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
)

type IssueAccessor struct{ root *filesystem.Root }

func NewIssueAccessor(root *filesystem.Root) *IssueAccessor { return &IssueAccessor{root: root} }

func issueDayDir(s observation.ServiceName, day time.Time) string {
	return path.Join(serviceDir(s), string(domain.DataIssues), periodName(domain.DataIssues, day))
}

func issueFile(s observation.ServiceName, day time.Time, name string) string {
	return path.Join(issueDayDir(s, day), pathName(name)+".json")
}

func (a *IssueAccessor) ReadIssue(ctx context.Context, s observation.ServiceName, day time.Time, name string) (domain.Issue, bool, error) {
	var is domain.Issue
	ok, err := readChecked(a.root, issueFile(s, day, name), &is)
	if err != nil {
		return domain.Issue{}, false, domain.ErrInternal.Wrap(err, "read an issue")
	}
	return is, ok, nil
}

func (a *IssueAccessor) WriteIssue(ctx context.Context, s observation.ServiceName, day time.Time, issue domain.Issue) error {
	b, err := checkedLine(issue)
	if err != nil {
		return domain.ErrInternal.Wrap(err, "encode an issue")
	}
	if err := a.root.Replace(issueFile(s, day, issue.Name), b); err != nil {
		return domain.ErrInternal.Wrap(err, "write an issue")
	}
	return nil
}

func (a *IssueAccessor) ListIssues(ctx context.Context, s observation.ServiceName, from, to time.Time) ([]domain.DatedIssue, error) {
	var out []domain.DatedIssue
	for _, day := range periods(domain.DataIssues, from, to, 0) {
		entries, err := a.root.ReadDir(issueDayDir(s, day))
		if err != nil {
			return nil, domain.ErrInternal.Wrap(err, "list issues")
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			var is domain.Issue
			ok, err := readChecked(a.root, path.Join(issueDayDir(s, day), e.Name()), &is)
			if err != nil {
				warnBroken(e.Name(), err)
				continue
			}
			if ok {
				out = append(out, domain.DatedIssue{Day: day, Issue: is})
			}
		}
	}
	return out, nil
}

type PreservedTraceAccessor struct{ root *filesystem.Root }

func NewPreservedTraceAccessor(root *filesystem.Root) *PreservedTraceAccessor {
	return &PreservedTraceAccessor{root: root}
}

func preservedDir(s observation.ServiceName, i observation.InstanceName, kind domain.PreservedKind, day time.Time) string {
	k := kind.DataKind()
	return path.Join(instanceDir(s, i), string(k), periodName(k, day))
}

func preservedFile(s observation.ServiceName, i observation.InstanceName, p domain.PreservedTrace) string {
	return path.Join(preservedDir(s, i, p.Kind, p.At), pathName(p.Dir), p.FileName()+".pb.gz")
}

func (a *PreservedTraceAccessor) SavePreservedTrace(ctx context.Context, s observation.ServiceName, i observation.InstanceName, p domain.PreservedTrace, chunk observation.TraceChunk) error {
	b, err := gzipMember(agentrpc.ToTraceChunk(chunk))
	if err != nil {
		return domain.ErrInternal.Wrap(err, "encode a preserved trace")
	}
	if _, err := a.root.Create(preservedFile(s, i, p), b); err != nil {
		return domain.ErrInternal.Wrap(err, "create a preserved trace")
	}
	return nil
}

func (a *PreservedTraceAccessor) readTrace(name string) (observation.TraceChunk, bool, error) {
	var (
		out observation.TraceChunk
		ok  bool
	)
	err := readMembers(a.root, name, func() *agentrpc.TraceChunk { return &agentrpc.TraceChunk{} }, func(m *agentrpc.TraceChunk) {
		out, ok = agentrpc.FromTraceChunk(m), true
	})
	return out, ok, err
}

func (a *PreservedTraceAccessor) list(s observation.ServiceName, i observation.InstanceName, kind domain.PreservedKind, day time.Time, dir string) ([]domain.PreservedTrace, error) {
	d := path.Join(preservedDir(s, i, kind, day), pathName(dir))
	entries, err := a.root.ReadDir(d)
	if err != nil {
		return nil, err
	}
	var out []domain.PreservedTrace
	for _, e := range entries {
		at, ok := domain.ParsePreservedTime(strings.TrimSuffix(e.Name(), ".pb.gz"))
		if !ok {
			continue
		}
		c, ok, err := a.readTrace(path.Join(d, e.Name()))
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, domain.PreservedTrace{Kind: kind, Dir: dir, At: at, From: c.StartedAt, To: c.EndedAt()})
		}
	}
	return out, nil
}

func (a *PreservedTraceAccessor) ListPreservedTraces(ctx context.Context, s observation.ServiceName, i observation.InstanceName, kind domain.PreservedKind, day time.Time, dir string) ([]domain.PreservedTrace, error) {
	out, err := a.list(s, i, kind, day, dir)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "list preserved traces")
	}
	return out, nil
}

func (a *PreservedTraceAccessor) ListPreservedTracesBetween(ctx context.Context, s observation.ServiceName, i observation.InstanceName, from, to time.Time) ([]domain.PreservedTrace, error) {
	var out []domain.PreservedTrace
	for _, kind := range []domain.PreservedKind{domain.PreservedAroundIssue, domain.PreservedAroundErrorLog} {
		for _, day := range periods(kind.DataKind(), from, to, 0) {
			dirs, err := a.root.ReadDir(preservedDir(s, i, kind, day))
			if err != nil {
				return nil, domain.ErrInternal.Wrap(err, "list preserved traces")
			}
			for _, d := range dirs {
				ps, err := a.list(s, i, kind, day, unPathName(d.Name()))
				if err != nil {
					return nil, domain.ErrInternal.Wrap(err, "list preserved traces")
				}
				out = append(out, ps...)
			}
		}
	}
	return out, nil
}

func (a *PreservedTraceAccessor) ReadPreservedTrace(ctx context.Context, s observation.ServiceName, i observation.InstanceName, p domain.PreservedTrace) (observation.TraceChunk, bool, error) {
	c, ok, err := a.readTrace(preservedFile(s, i, p))
	if err != nil {
		return observation.TraceChunk{}, false, domain.ErrInternal.Wrap(err, "read a preserved trace")
	}
	return c, ok, nil
}
