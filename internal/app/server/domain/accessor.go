//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_accessor.go -package=mock
package domain

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// TimeRange は [From, To]
type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type InstanceLocker interface {
	// LockInstance は 1 つのインスタンスの書き込みを 1 つずつにする。返した関数で外す
	LockInstance(service observation.ServiceName, instance observation.InstanceName) func()
}

type HoldingAccessor interface {
	Holding(ctx context.Context, service observation.ServiceName, instance observation.InstanceName) (Holding, error)
	RememberHolding(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, h Holding)
	// ForgetHolding は書きかけで失敗したときに、次はディスクから引き直させる
	ForgetHolding(ctx context.Context, service observation.ServiceName, instance observation.InstanceName)
}

type InventoryAccessor interface {
	ListServices(ctx context.Context) ([]observation.ServiceName, error)
	ListInstances(ctx context.Context, service observation.ServiceName) ([]observation.InstanceName, error)
}

type LogAccessor interface {
	AppendLogs(ctx context.Context, instance observation.Instance, logs []observation.LogLine) error
	// ListLogs は [from, to) のログを CompareLogs の順で返す
	ListLogs(ctx context.Context, service observation.ServiceName, from, to time.Time) ([]StoredLog, error)
}

type TraceAccessor interface {
	AppendTraceChunks(ctx context.Context, instance observation.Instance, chunks []observation.TraceChunk) error
	// ListTraceChunks は [from, to] に重なるチャンクを時刻順に返す
	ListTraceChunks(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, from, to time.Time) ([]observation.TraceChunk, error)
	ListTraceRanges(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, from, to time.Time) ([]TimeRange, error)
}

type SnapshotAccessor interface {
	AppendSnapshots(ctx context.Context, instance observation.Instance, snapshots []observation.GoroutineSnapshot) error
	ListSnapshots(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, from, to time.Time) ([]observation.GoroutineSnapshot, error)
}

type LeakCheckAccessor interface {
	AppendLeakChecks(ctx context.Context, instance observation.Instance, checks []observation.GoroutineLeakCheck) error
	ListLeakChecks(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, from, to time.Time) ([]observation.GoroutineLeakCheck, error)
}

type LifecycleAccessor interface {
	AppendLifecycle(ctx context.Context, service observation.ServiceName, events []LifecycleEvent) error
	ListLifecycle(ctx context.Context, service observation.ServiceName, from, to time.Time) ([]LifecycleEvent, error)
	LastLifecycle(ctx context.Context, service observation.ServiceName, instance observation.InstanceName) (LifecycleEvent, bool, error)
}

type MetricAccessor interface {
	AppendMetrics(ctx context.Context, service observation.ServiceName, samples []MetricSample) error
	ListMetrics(ctx context.Context, service observation.ServiceName, from, to time.Time) ([]MetricSample, error)
}

type BreakdownAccessor interface {
	AppendBreakdowns(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, rows []MinuteBreakdown) error
	ListBreakdowns(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, from, to time.Time) ([]MinuteBreakdown, error)
}

// DatedIssue は 1 日分の Issue のファイル
type DatedIssue struct {
	Day   time.Time
	Issue Issue
}

type IssueAccessor interface {
	ReadIssue(ctx context.Context, service observation.ServiceName, day time.Time, name string) (Issue, bool, error)
	WriteIssue(ctx context.Context, service observation.ServiceName, day time.Time, issue Issue) error
	// ListIssues は [from, to] の日のファイルを日の順に返す
	ListIssues(ctx context.Context, service observation.ServiceName, from, to time.Time) ([]DatedIssue, error)
}

type PreservedTraceAccessor interface {
	// SavePreservedTrace は新しく作る。同じ名前がもうあれば何もしない
	SavePreservedTrace(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, p PreservedTrace, chunk observation.TraceChunk) error
	ListPreservedTraces(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, kind PreservedKind, day time.Time, dir string) ([]PreservedTrace, error)
	// ListPreservedTracesBetween は [from, to] の日に残したものをすべて返す
	ListPreservedTracesBetween(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, from, to time.Time) ([]PreservedTrace, error)
	ReadPreservedTrace(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, p PreservedTrace) (observation.TraceChunk, bool, error)
}

type LogWriterAccessor interface {
	AppendLogWriters(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, writers []LogWriter) error
	ListLogWriters(ctx context.Context, service observation.ServiceName, instance observation.InstanceName, from, to time.Time) ([]LogWriter, error)
}

type CheckpointAccessor interface {
	ReadCheckpoints(ctx context.Context, service observation.ServiceName) (Checkpoints, error)
	// UpdateCheckpoint は 1 つの処理・キーのチェックポイントだけを置き換える。ほかの処理が同時に進めても失わない
	UpdateCheckpoint(ctx context.Context, service observation.ServiceName, process Process, key string, cp Checkpoint) error
	// ModifyCheckpoints は読んでから書き直すまでを、ほかの処理の更新と重ねずに行う。空になればファイルを消す
	ModifyCheckpoints(ctx context.Context, service observation.ServiceName, modify func(Checkpoints) Checkpoints) error
}

type DataFileAccessor interface {
	ListDataFiles(ctx context.Context) ([]DataFile, error)
	// RemoveDataFiles は消してから、空になったディレクトリも消す
	RemoveDataFiles(ctx context.Context, files []DataFile) error
}
