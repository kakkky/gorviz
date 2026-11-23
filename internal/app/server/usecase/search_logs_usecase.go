package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type SearchLogsUsecaseInput struct {
	Service  observation.ServiceName
	From     time.Time
	To       time.Time
	Filter   domain.LogFilter
	After    domain.LogID
	MaxBytes int
	Now      time.Time
}

// SearchLogsUsecaseOutput の Next は続きを取るときに After に渡す ID。続きがなければ空
type SearchLogsUsecaseOutput struct {
	From          time.Time
	To            time.Time
	Logs          []domain.StoredLog
	Next          domain.LogID
	RetainedSince time.Time
}

type SearchLogsUsecase struct {
	retention domain.Retention
	inventory domain.InventoryAccessor
	logs      domain.LogAccessor
}

func (us *UseCase) NewSearchLogsUsecase() *SearchLogsUsecase {
	return &SearchLogsUsecase{retention: us.settings.Retention, inventory: us.inventory, logs: us.logs}
}

func logRange(from, to, now time.Time) (time.Time, time.Time, error) {
	if to.IsZero() {
		to = now
	}
	if from.IsZero() {
		from = to.Add(-time.Hour)
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, domain.ErrInvalidArgument.With("from must be before to")
	}
	return from, to, nil
}

func (u *SearchLogsUsecase) Exec(ctx context.Context, in SearchLogsUsecaseInput) (SearchLogsUsecaseOutput, error) {
	if err := requireService(ctx, u.inventory, in.Service); err != nil {
		return SearchLogsUsecaseOutput{}, err
	}
	from, to, err := logRange(in.From, in.To, in.Now)
	if err != nil {
		return SearchLogsUsecaseOutput{}, err
	}
	var after domain.StoredLog
	start := from
	if in.After != "" {
		p, err := domain.ParseLogID(in.After)
		if err != nil {
			return SearchLogsUsecaseOutput{}, err
		}
		after = domain.StoredLog{Instance: p.Instance, LogLine: observation.LogLine{At: p.At, Agent: p.Agent, Seq: p.Seq}}
		start = maxTime(from, p.At)
	}
	maxBytes := in.MaxBytes
	if maxBytes <= 0 {
		maxBytes = domain.DefaultPageBytes
	}
	out := SearchLogsUsecaseOutput{From: from, To: to, Logs: []domain.StoredLog{}, RetainedSince: u.retention.RetainedSince(domain.DataLogs, in.Now)}
	size := 0
	for t := start; t.Before(to); t = domain.DataLogs.PeriodStart(t).Add(time.Hour) {
		end := minTime(to, domain.DataLogs.PeriodStart(t).Add(time.Hour))
		logs, err := u.logs.ListLogs(ctx, in.Service, t, end)
		if err != nil {
			return SearchLogsUsecaseOutput{}, err
		}
		for _, l := range logs {
			if in.After != "" && domain.CompareLogs(l.LogLine, after.LogLine) <= 0 {
				continue
			}
			if !in.Filter.Match(l) {
				continue
			}
			if size+l.SummarySize() > maxBytes && len(out.Logs) > 0 {
				out.Next = out.Logs[len(out.Logs)-1].ID()
				return out, nil
			}
			size += l.SummarySize()
			out.Logs = append(out.Logs, l)
		}
	}
	return out, nil
}
