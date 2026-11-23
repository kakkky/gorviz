package accessor

import (
	"context"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type LogAccessor struct{ root *filesystem.Root }

func NewLogAccessor(root *filesystem.Root) *LogAccessor { return &LogAccessor{root: root} }

func (a *LogAccessor) AppendLogs(ctx context.Context, instance observation.Instance, logs []observation.LogLine) error {
	byFile := map[string][]domain.StoredLog{}
	var order []string
	for _, l := range logs {
		f := serviceFile(instance.Service, domain.DataLogs, l.At, ".jsonl")
		if _, ok := byFile[f]; !ok {
			order = append(order, f)
		}
		byFile[f] = append(byFile[f], domain.StoredLog{Instance: instance.Name, Incarnation: instance.Incarnation, LogLine: l})
	}
	for _, f := range order {
		b, err := checkedLines(byFile[f])
		if err != nil {
			return domain.ErrInternal.Wrap(err, "encode log lines")
		}
		if _, err := a.root.Append(f, b, validLines); err != nil {
			return domain.ErrInternal.Wrap(err, "append log lines")
		}
	}
	return nil
}

func (a *LogAccessor) ListLogs(ctx context.Context, service observation.ServiceName, from, to time.Time) ([]domain.StoredLog, error) {
	var out []domain.StoredLog
	for _, p := range periods(domain.DataLogs, from, to, 0) {
		if err := ctx.Err(); err != nil {
			return nil, domain.ErrInternal.Wrap(err, "list logs")
		}
		err := eachLine(a.root, serviceFile(service, domain.DataLogs, p, ".jsonl"), func(l domain.StoredLog) {
			if !l.At.Before(from) && l.At.Before(to) {
				out = append(out, l)
			}
		})
		if err != nil {
			return nil, domain.ErrInternal.Wrap(err, "read logs")
		}
	}
	slices.SortStableFunc(out, func(a, b domain.StoredLog) int { return domain.CompareLogs(a.LogLine, b.LogLine) })
	return out, nil
}
