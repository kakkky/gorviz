package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// leakLookback はリーク判定をさかのぼって探す長さ。goroutineleak プロファイルは間を空けて取るため
const leakLookback = time.Hour

// stateLookback は期間の頭の状態に入った時刻を探して、execution trace をさかのぼる長さ。trace は重いので短く区切り、続きは画面からさらにさかのぼる
const stateLookback = 5 * time.Minute

type DiagnoseGoroutineUsecaseInput struct {
	Service   observation.ServiceName
	Instance  observation.InstanceName
	Goroutine observation.GoroutineID
	Period    PeriodQuery
	Now       time.Time
}

// Unavailable は保持期間を過ぎて (または残っていなくて) 出せない項目と、いつからなら出せるか
type Unavailable struct {
	Field         string
	Reason        string
	RetainedSince time.Time
}

const (
	ReasonPastRetention = "past-retention"
	ReasonNotRecorded   = "not-recorded"
)

// DiagnoseGoroutineUsecaseOutput の Found が false なら、期間について残っているどのデータにもその goroutine が現れなかった。
// TraceFromPreserved は Issue の発生・エラーログの前後に残した execution trace から算出したこと
type DiagnoseGoroutineUsecaseOutput struct {
	Period             domain.Period
	Found              bool
	Diagnosis          domain.Diagnosis
	TraceFromPreserved bool
	Leak               domain.LeakStatus
	Logs               []domain.StoredLog
	Unavailable        []Unavailable
	NearbyTraces       []domain.PreservedTrace
	StateBefore        *StateBefore
}

// StateBefore は期間の頭で g がいた状態と、それに入った時刻。Began が false なら、さかのぼった trace の頭 (RecordedFrom) より前から続いている。
// Earlier はそれより前の trace も残っているかもしれないこと (さかのぼる長さで区切った)
type StateBefore struct {
	domain.StateSince
	RecordedFrom time.Time
	Earlier      bool
}

type DiagnoseGoroutineUsecase struct {
	retention  domain.Retention
	inventory  domain.InventoryAccessor
	period     periodDeps
	source     traceSource
	breakdowns domain.BreakdownAccessor
	snapshots  domain.SnapshotAccessor
	leakChecks domain.LeakCheckAccessor
	logs       domain.LogAccessor
	logWriters domain.LogWriterAccessor
}

func (us *UseCase) NewDiagnoseGoroutineUsecase() *DiagnoseGoroutineUsecase {
	src := traceSource{traces: us.traces, preserved: us.preserved}
	return &DiagnoseGoroutineUsecase{retention: us.settings.Retention, inventory: us.inventory, period: periodDeps{logs: us.logs, issues: us.issues, traces: src},
		source: src, breakdowns: us.breakdowns, snapshots: us.snapshots, leakChecks: us.leakChecks, logs: us.logs, logWriters: us.logWriters}
}

func (u *DiagnoseGoroutineUsecase) Exec(ctx context.Context, in DiagnoseGoroutineUsecaseInput) (DiagnoseGoroutineUsecaseOutput, error) {
	if err := requireInstance(ctx, u.inventory, in.Service, in.Instance); err != nil {
		return DiagnoseGoroutineUsecaseOutput{}, err
	}
	p, err := u.period.decidePeriod(ctx, in.Service, in.Instance, in.Goroutine, in.Period, in.Now)
	if err != nil {
		return DiagnoseGoroutineUsecaseOutput{}, err
	}
	out := DiagnoseGoroutineUsecaseOutput{Period: p}
	s, i, g := in.Service, in.Instance, in.Goroutine
	joined, preserved, ok, err := u.source.trace(ctx, s, i, p.From, p.To)
	if err != nil {
		return DiagnoseGoroutineUsecaseOutput{}, err
	}
	if ok {
		out.Diagnosis, out.Found = domain.DiagnoseTrace(joined, g, p.From, p.To)
		out.TraceFromPreserved = preserved
	}
	if out.Found && out.Diagnosis.Source == domain.SourceExecutionTrace {
		out.StateBefore, err = u.stateBefore(ctx, s, i, g, p.From)
		if err != nil {
			return DiagnoseGoroutineUsecaseOutput{}, err
		}
	}
	if !out.Found || preserved {
		out.Unavailable = append(out.Unavailable, u.unavailable("schedulerReplay", domain.DataExecutionTraces, p.From, in.Now))
		near, err := u.source.nearbyTraces(ctx, s, i, p.From)
		if err != nil {
			return DiagnoseGoroutineUsecaseOutput{}, err
		}
		out.NearbyTraces = near
	}
	if !out.Found {
		rows, err := u.breakdowns.ListBreakdowns(ctx, s, i, p.From.Truncate(domain.BreakdownStep), p.To)
		if err != nil {
			return DiagnoseGoroutineUsecaseOutput{}, err
		}
		out.Diagnosis, out.Found = domain.DiagnoseBreakdowns(rows, g, p.From, p.To)
	}
	snapshots, err := u.snapshots.ListSnapshots(ctx, s, i, p.From.Add(-leakLookback), p.To.Add(time.Minute))
	if err != nil {
		return DiagnoseGoroutineUsecaseOutput{}, err
	}
	if !out.Found {
		out.Unavailable = append(out.Unavailable, u.unavailable("breakdown", domain.DataGoroutineStateBreakdowns, p.From, in.Now))
		out.Diagnosis, out.Found = domain.DiagnoseSnapshots(snapshots, g, p.From, p.To)
	} else if out.Diagnosis.CreatedBy.Function == "" {
		if d, ok := domain.DiagnoseSnapshots(snapshots, g, p.From, p.To); ok {
			out.Diagnosis.CreatedBy = d.CreatedBy
		}
	}
	checks, err := u.leakChecks.ListLeakChecks(ctx, s, i, p.To.Add(-leakLookback), p.To)
	if err != nil {
		return DiagnoseGoroutineUsecaseOutput{}, err
	}
	out.Leak = domain.DiagnoseLeak(checks, snapshots, g, p.To)
	if !out.Leak.Checked && out.Leak.Status == "" {
		out.Unavailable = append(out.Unavailable, u.unavailable("leak", domain.DataGoroutineLeakChecks, p.To, in.Now))
	}
	logs, err := writtenLogs(ctx, u.logs, u.logWriters, s, i, g, p)
	if err != nil {
		return DiagnoseGoroutineUsecaseOutput{}, err
	}
	out.Logs = logs
	return out, nil
}

func (u *DiagnoseGoroutineUsecase) stateBefore(ctx context.Context, s observation.ServiceName, i observation.InstanceName, g observation.GoroutineID, at time.Time) (*StateBefore, error) {
	from := at.Add(-stateLookback)
	joined, _, ok, err := u.source.trace(ctx, s, i, from, at)
	if err != nil || !ok {
		return nil, err
	}
	st, ok := domain.StateSinceAt(joined, g)
	if !ok {
		return nil, nil
	}
	return &StateBefore{StateSince: st, RecordedFrom: joined.StartedAt, Earlier: !joined.StartedAt.After(from)}, nil
}

func (u *DiagnoseGoroutineUsecase) unavailable(field string, k domain.DataKind, at, now time.Time) Unavailable {
	return unavailableOf(u.retention, field, k, at, now)
}

// writtenLogs は g が期間に書いたログ
func writtenLogs(ctx context.Context, logs domain.LogAccessor, writers domain.LogWriterAccessor, s observation.ServiceName, i observation.InstanceName, g observation.GoroutineID, p domain.Period) ([]domain.StoredLog, error) {
	ws, err := writers.ListLogWriters(ctx, s, i, p.From, p.To)
	if err != nil {
		return nil, err
	}
	mine := map[domain.LogID]bool{}
	for _, w := range ws {
		if w.Found && w.Goroutine == g {
			mine[w.Log] = true
		}
	}
	if len(mine) == 0 {
		return nil, nil
	}
	all, err := logs.ListLogs(ctx, s, p.From, p.To.Add(time.Nanosecond))
	if err != nil {
		return nil, err
	}
	var out []domain.StoredLog
	for _, l := range all {
		if mine[l.ID()] {
			out = append(out, l)
		}
	}
	return out, nil
}
