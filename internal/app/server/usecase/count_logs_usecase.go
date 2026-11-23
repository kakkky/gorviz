package usecase

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

const countValuesLimit = 100

type CountLogsUsecaseInput struct {
	Service observation.ServiceName
	From    time.Time
	To      time.Time
	Filter  domain.LogFilter
	By      string
	Step    time.Duration
	Now     time.Time
}

type LogCount struct {
	Value string
	Count int
}

// LogBucket は Step ごとの区切り 1 つの件数。Counts は By の値ごと (キーを持たないログは空文字)
type LogBucket struct {
	At     time.Time
	Total  int
	Counts map[string]int
}

// CountLogsUsecaseOutput の Missing はキーを持たなかったログの数、Others は上位に入らなかった値の件数の合計。
// Buckets は Input の Step を渡したときだけ、From から Step ごとに区切った件数の推移
type CountLogsUsecaseOutput struct {
	From          time.Time
	To            time.Time
	By            string
	Total         int
	Counts        []LogCount
	Missing       int
	Others        int
	Buckets       []LogBucket
	RetainedSince time.Time
}

type CountLogsUsecase struct {
	retention domain.Retention
	inventory domain.InventoryAccessor
	logs      domain.LogAccessor
}

func (us *UseCase) NewCountLogsUsecase() *CountLogsUsecase {
	return &CountLogsUsecase{retention: us.settings.Retention, inventory: us.inventory, logs: us.logs}
}

func (u *CountLogsUsecase) Exec(ctx context.Context, in CountLogsUsecaseInput) (CountLogsUsecaseOutput, error) {
	if in.By == "" {
		return CountLogsUsecaseOutput{}, domain.ErrInvalidArgument.With("by is required: level, instance, message, message-shape or a JSON key")
	}
	if err := requireService(ctx, u.inventory, in.Service); err != nil {
		return CountLogsUsecaseOutput{}, err
	}
	from, to, err := logRange(in.From, in.To, in.Now)
	if err != nil {
		return CountLogsUsecaseOutput{}, err
	}
	logs, err := u.logs.ListLogs(ctx, in.Service, from, to)
	if err != nil {
		return CountLogsUsecaseOutput{}, err
	}
	out := CountLogsUsecaseOutput{From: from, To: to, By: in.By, RetainedSince: u.retention.RetainedSince(domain.DataLogs, in.Now)}
	if in.Step > 0 {
		for t := from; t.Before(to); t = t.Add(in.Step) {
			out.Buckets = append(out.Buckets, LogBucket{At: t, Counts: map[string]int{}})
		}
	}
	counts := map[string]int{}
	for _, l := range logs {
		if !in.Filter.Match(l) {
			continue
		}
		out.Total++
		v, ok := domain.LogKey(l, in.By)
		if n := int(l.At.Sub(from) / max(in.Step, 1)); in.Step > 0 && n >= 0 && n < len(out.Buckets) {
			out.Buckets[n].Total++
			out.Buckets[n].Counts[v]++
		}
		if !ok {
			out.Missing++
			continue
		}
		counts[v]++
	}
	for v, n := range counts {
		out.Counts = append(out.Counts, LogCount{Value: v, Count: n})
	}
	slices.SortFunc(out.Counts, func(a, b LogCount) int { return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Value, b.Value)) })
	if len(out.Counts) > countValuesLimit {
		for _, c := range out.Counts[countValuesLimit:] {
			out.Others += c.Count
		}
		out.Counts = out.Counts[:countValuesLimit]
	}
	return out, nil
}
