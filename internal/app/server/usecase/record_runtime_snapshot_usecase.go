package usecase

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const (
	goroutineSamplesKey = "runtime-snapshot:goroutine-samples"
	lastDumpAtKey       = "runtime-snapshot:last-dump-at"
	// latestGoroutinesKey は直近の goroutine dump。録画した trace のラベル付けに、Live 以外 (Explore) からも使う
	latestGoroutinesKey = "runtime-snapshot:latest-goroutines"
	latestSnapshotKey   = "runtime-snapshot:latest"
	topOriginsLimit     = 10
)

type RecordRuntimeSnapshotUsecase struct {
	dumpSource     domain.DumpSource
	inMemoryKVS    domain.InMemoryKVS
	traceRecording domain.TraceRecording
	traceWindow    time.Duration
	historySize    int
	leakPolicy     domain.LeakDetectionPolicy
	// 履歴の読み書きが混ざると件数の推移が壊れるので、Exec は直列にする
	mu *sync.Mutex
}

func (us *UseCase) NewRecordRuntimeSnapshotUsecase() *RecordRuntimeSnapshotUsecase {
	return &RecordRuntimeSnapshotUsecase{
		dumpSource:     us.dumpSource,
		inMemoryKVS:    us.inMemoryKVS,
		traceRecording: us.traceRecording,
		traceWindow:    us.settings.TraceWindow,
		historySize:    us.settings.HistorySize,
		leakPolicy:     us.settings.Leak,
		mu:             &us.snapshotMu,
	}
}

type RecordRuntimeSnapshotUsecaseInput struct {
	Now time.Time
}

type RecordRuntimeSnapshotUsecaseOutput struct {
	Snapshot RuntimeSnapshot
}

// RuntimeSnapshot は観測 1 回分の結果。録画ループが定期的に作って置き、Live / CLI はそれを読むだけにする。
// goroutine dump は毎回は取らないので、DumpAt はスナップショットの時刻 At より古いことがある
type RuntimeSnapshot struct {
	At       time.Time
	DumpAt   time.Time
	Timeline model.SchedulerTimeline
	// TimelineStartedAt は Timeline の時刻 0 に当たる壁時計の時刻
	TimelineStartedAt time.Time
	// Runtime は直近の録画の区切り 1 つ分から見た、ヒープ・GC・CPU 待ちの量
	Runtime        RuntimeLoad
	GoroutineCount int
	StateCounts    domain.GoroutineStateCounts
	TopOrigins     []RuntimeSnapshotOriginCount
	// Recording は今持てている録画の区間。画面に示す
	Recording        model.RecordingSpan
	HasRecording     bool
	Leaks            []domain.Leak
	LeakCheckStatus  model.LeakCheckStatus
	LeakedGoroutines []model.GoroutineID
	// GoroutineHistory は直近の観測ごとの G の総数。リークは「右肩上がり」で見えるので推移を返す
	GoroutineHistory []int
}

type RuntimeLoad struct {
	HeapLiveBytes uint64
	HeapGoalBytes uint64
	GCPerSecond   float64
	// CPUWaiting は P を待っている G の数の平均
	CPUWaiting float64
}

func runtimeLoadOf(c model.TraceChunk) RuntimeLoad {
	w := c.Timeline.Window
	if w <= 0 {
		return RuntimeLoad{}
	}
	load := RuntimeLoad{
		GCPerSecond: float64(c.Timeline.GCCycles) / w.Seconds(),
		CPUWaiting:  float64(domain.TotalByCause(c.Timeline)[domain.CauseCPUWait]) / float64(w),
	}
	if n := len(c.Timeline.Heap); n > 0 {
		load.HeapLiveBytes, load.HeapGoalBytes = c.Timeline.Heap[n-1].LiveBytes, c.Timeline.Heap[n-1].GoalBytes
	}
	return load
}

type RuntimeSnapshotOriginCount struct {
	Origin model.GoroutineOrigin
	Count  int
}

type goroutineSampleHistory struct {
	samples []domain.GoroutineSample
	totals  []int
}

func (us *RecordRuntimeSnapshotUsecase) Exec(_ context.Context, in RecordRuntimeSnapshotUsecaseInput) (RecordRuntimeSnapshotUsecaseOutput, error) {
	us.mu.Lock()
	defer us.mu.Unlock()

	// dump は agent が取る。server は agent の最新の dump を読み、新しいものだけを履歴に足す
	dump, ok := us.dumpSource.LatestDump()
	if !ok {
		return RecordRuntimeSnapshotUsecaseOutput{}, model.ErrNotFound.With("the agent has not taken a goroutine dump yet")
	}
	goroutines := dump.Goroutines
	lastDumpAt := domain.KVSGetOr(us.inMemoryKVS, lastDumpAtKey, time.Time{})
	dumped := dump.At.After(lastDumpAt)
	us.inMemoryKVS.Set(lastDumpAtKey, dump.At)
	us.inMemoryKVS.Set(latestGoroutinesKey, goroutines)

	// trace は録画ループが取り続けているので、ここでは取らずに最新の区切りの末尾を使う
	var (
		timeline  model.SchedulerTimeline
		startedAt time.Time
		load      RuntimeLoad
	)
	if chunk, ok := us.traceRecording.Latest(); ok {
		w := chunk.Timeline.Window
		timeline = chunk.Timeline.Slice(w-us.traceWindow, w)
		startedAt = chunk.StartedAt.Add(max(w-us.traceWindow, 0))
		load = runtimeLoadOf(chunk)
		timeline.Entries = maps.Clone(timeline.Entries)
		domain.EnrichFromDump(&timeline, goroutines)
	}

	history := domain.KVSGetOr(us.inMemoryKVS, goroutineSamplesKey, goroutineSampleHistory{})
	if dumped {
		history.samples = appendBounded(history.samples, domain.NewGoroutineSample(dump.At, goroutines), us.historySize)
		history.totals = appendBounded(history.totals, len(goroutines), us.historySize)
		us.inMemoryKVS.Set(goroutineSamplesKey, history)
	}

	snapshot := RuntimeSnapshot{
		At:                in.Now,
		DumpAt:            dump.At,
		TimelineStartedAt: startedAt,
		Runtime:           load,
		Timeline:          timeline,
		GoroutineCount:    len(goroutines),
		StateCounts:       domain.CountGoroutineStates(goroutines),
		TopOrigins:        topOrigins(goroutines, topOriginsLimit),
		Leaks:             domain.DetectLeaks(history.samples, goroutines, us.leakPolicy),
		LeakCheckStatus:   dump.LeakCheck,
		GoroutineHistory:  slices.Clone(history.totals),
		LeakedGoroutines:  leakedIDs(goroutines),
	}
	snapshot.Recording, snapshot.HasRecording = us.traceRecording.Span()
	us.inMemoryKVS.Set(latestSnapshotKey, snapshot)
	return RecordRuntimeSnapshotUsecaseOutput{Snapshot: snapshot}, nil
}

func leakedIDs(gs []model.Goroutine) []model.GoroutineID {
	var ids []model.GoroutineID
	for _, g := range gs {
		if g.Leaked {
			ids = append(ids, g.ID)
		}
	}
	return ids
}

func topOrigins(gs []model.Goroutine, limit int) []RuntimeSnapshotOriginCount {
	counts := map[model.GoroutineOrigin]int{}
	for _, g := range gs {
		counts[g.Origin()]++
	}
	out := make([]RuntimeSnapshotOriginCount, 0, len(counts))
	for o, c := range counts {
		out = append(out, RuntimeSnapshotOriginCount{Origin: o, Count: c})
	}
	slices.SortFunc(out, func(a, b RuntimeSnapshotOriginCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Origin, b.Origin))
	})
	return out[:min(limit, len(out))]
}

func appendBounded[T any](s []T, v T, size int) []T {
	s = append(s, v)
	if len(s) > size {
		s = s[len(s)-size:]
	}
	return s
}
