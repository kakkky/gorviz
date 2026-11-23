package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type GetLogUsecaseInput struct {
	Service observation.ServiceName
	Log     domain.LogID
	Now     time.Time
}

// WriterStatus は書いた goroutine がどうなっているか
type WriterStatus string

const (
	WriterFound         WriterStatus = "found"
	WriterUnknown       WriterStatus = "unknown"
	WriterPending       WriterStatus = "pending"
	WriterPastRetention WriterStatus = "past-retention"
)

// GetLogUsecaseOutput の Period はこのログから goroutine を開いたときに調べる期間。Preserved はエラーログの前後に残した execution trace
type GetLogUsecaseOutput struct {
	Log          domain.StoredLog
	Attrs        map[string]string
	Writer       domain.LogWriter
	WriterStatus WriterStatus
	Period       domain.Period
	Preserved    []domain.PreservedTrace
}

type GetLogUsecase struct {
	retention  domain.Retention
	inventory  domain.InventoryAccessor
	logs       domain.LogAccessor
	logWriters domain.LogWriterAccessor
	preserved  domain.PreservedTraceAccessor
}

func (us *UseCase) NewGetLogUsecase() *GetLogUsecase {
	return &GetLogUsecase{retention: us.settings.Retention, inventory: us.inventory, logs: us.logs, logWriters: us.logWriters, preserved: us.preserved}
}

func (u *GetLogUsecase) Exec(ctx context.Context, in GetLogUsecaseInput) (GetLogUsecaseOutput, error) {
	if err := requireService(ctx, u.inventory, in.Service); err != nil {
		return GetLogUsecaseOutput{}, err
	}
	l, err := findLog(ctx, u.logs, in.Service, in.Log)
	if err != nil {
		return GetLogUsecaseOutput{}, err
	}
	from, to := domain.LogPeriod(l.LogLine)
	out := GetLogUsecaseOutput{Log: l, Attrs: l.Attrs(), Period: domain.Period{From: from, To: to, By: domain.PeriodLog}, WriterStatus: WriterPending}
	writers, err := u.logWriters.ListLogWriters(ctx, in.Service, l.Instance, l.At, l.At)
	if err != nil {
		return GetLogUsecaseOutput{}, err
	}
	for _, w := range writers {
		if w.Log == l.ID() {
			out.Writer, out.WriterStatus = w, WriterUnknown
			if w.Found {
				out.WriterStatus = WriterFound
			}
		}
	}
	if out.WriterStatus == WriterPending && !u.retention.Keeps(domain.DataLogWriterGoroutines, l.At, in.Now) {
		out.WriterStatus = WriterPastRetention
	}
	if l.Level.AtLeast(observation.LogLevelError) {
		dir := domain.FileName(domain.MessageShape(l.Message))
		ps, err := u.preserved.ListPreservedTraces(ctx, in.Service, l.Instance, domain.PreservedAroundErrorLog, domain.DataExecutionTracesAroundErrorLogs.PeriodStart(l.At), dir)
		if err != nil {
			return GetLogUsecaseOutput{}, err
		}
		for _, p := range ps {
			if !p.From.After(l.At) && !p.To.Before(l.At) {
				out.Preserved = append(out.Preserved, p)
			}
		}
	}
	return out, nil
}
