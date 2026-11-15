package usecase

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/kakkky/gorviz/domain"
)

const (
	goroutineSamplesKey   = "runtime-snapshot:goroutine-samples"
	dumpCountKey          = "runtime-snapshot:dump-count"
	lastDumpAtKey         = "runtime-snapshot:last-dump-at"
	leakCheckStatusKey    = "runtime-snapshot:leak-check-status"
	leakCheckSupportedKey = "runtime-snapshot:leak-check-supported"
	leakedGoroutinesKey   = "runtime-snapshot:leaked-goroutines"
	// latestGoroutinesKey は直近の goroutine dump。録画した trace のラベル付けに、Live 以外 (Explore) からも使う
	latestGoroutinesKey = "runtime-snapshot:latest-goroutines"
	latestSnapshotKey   = "runtime-snapshot:latest"
	topOriginsLimit     = 10
)

type LeakCheckStatus string

const (
	LeakCheckDone        LeakCheckStatus = "done"
	LeakCheckSkipped     LeakCheckStatus = "skipped"
	LeakCheckUnsupported LeakCheckStatus = "unsupported"
	LeakCheckDisabled    LeakCheckStatus = "disabled"
)

type RecordRuntimeSnapshotUsecase struct {
	runtimeClient  domain.RuntimeClient
	inMemoryKVS    domain.InMemoryKVS
	traceRecording domain.TraceRecording
	traceWindow    time.Duration
	dumpInterval   time.Duration
	historySize    int
	leakCheckEvery int
	leakPolicy     domain.LeakDetectionPolicy
	// 履歴の読み書きが混ざると件数の推移が壊れるので、Exec は直列にする
	mu *sync.Mutex
}

func (us *UseCase) NewRecordRuntimeSnapshotUsecase() *RecordRuntimeSnapshotUsecase {
	return &RecordRuntimeSnapshotUsecase{
		runtimeClient:  us.runtimeClient,
		inMemoryKVS:    us.inMemoryKVS,
		traceRecording: us.traceRecording,
		traceWindow:    us.cfg.TraceWindow,
		dumpInterval:   us.cfg.DumpInterval,
		historySize:    us.cfg.HistorySize,
		leakCheckEvery: us.cfg.LeakCheckEvery,
		leakPolicy: domain.LeakDetectionPolicy{
			MinSamples: us.cfg.LeakMinSamples,
			MinGrowth:  us.cfg.LeakMinGrowth,
		},
		mu: &us.snapshotMu,
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
	At               time.Time
	DumpAt           time.Time
	Timeline         domain.SchedulerTimeline
	GoroutineCount   int
	StateCounts      domain.GoroutineStateCounts
	TopOrigins       []RuntimeSnapshotOriginCount
	Leaks            []domain.Leak
	LeakCheckStatus  LeakCheckStatus
	LeakedGoroutines []domain.GoroutineID
	// GoroutineHistory は直近の観測ごとの G の総数。リークは「右肩上がり」で見えるので推移を返す
	GoroutineHistory []int
}

type RuntimeSnapshotOriginCount struct {
	Origin domain.GoroutineOrigin
	Count  int
}

type goroutineSampleHistory struct {
	samples []domain.GoroutineSample
	totals  []int
}

func (us *RecordRuntimeSnapshotUsecase) Exec(ctx context.Context, in RecordRuntimeSnapshotUsecaseInput) (RecordRuntimeSnapshotUsecaseOutput, error) {
	us.mu.Lock()
	defer us.mu.Unlock()

	lastDumpAt, dumped := getOr(us.inMemoryKVS, lastDumpAtKey, time.Time{}), false
	goroutines := getOr(us.inMemoryKVS, latestGoroutinesKey, []domain.Goroutine(nil))
	leakCheckStatus := getOr(us.inMemoryKVS, leakCheckStatusKey, LeakCheckSkipped)
	if lastDumpAt.IsZero() || in.Now.Sub(lastDumpAt) >= us.dumpInterval {
		dumpCount := getOr(us.inMemoryKVS, dumpCountKey, 0) + 1
		us.inMemoryKVS.Set(dumpCountKey, dumpCount)
		gs, status, err := us.fetchGoroutines(ctx, us.leakCheckStatusFor(dumpCount))
		if err != nil {
			return RecordRuntimeSnapshotUsecaseOutput{}, err
		}
		goroutines, leakCheckStatus, lastDumpAt, dumped = us.carryOverLeaked(gs), status, in.Now, true
		us.inMemoryKVS.Set(latestGoroutinesKey, goroutines)
		us.inMemoryKVS.Set(lastDumpAtKey, lastDumpAt)
		us.inMemoryKVS.Set(leakCheckStatusKey, leakCheckStatus)
	}

	// trace は録画ループが取り続けているので、ここでは取らずに最新の区切りの末尾を使う
	var timeline domain.SchedulerTimeline
	if chunk, ok := us.traceRecording.Latest(); ok {
		w := chunk.Timeline.Window
		timeline = chunk.Timeline.Slice(w-us.traceWindow, w)
		timeline.Entries = maps.Clone(timeline.Entries)
		timeline.EnrichFromDump(goroutines)
	}

	history := getOr(us.inMemoryKVS, goroutineSamplesKey, goroutineSampleHistory{})
	if dumped {
		history.samples = appendBounded(history.samples, domain.NewGoroutineSample(in.Now, goroutines), us.historySize)
		history.totals = appendBounded(history.totals, len(goroutines), us.historySize)
		us.inMemoryKVS.Set(goroutineSamplesKey, history)
	}

	snapshot := RuntimeSnapshot{
		At:               in.Now,
		DumpAt:           lastDumpAt,
		Timeline:         timeline,
		GoroutineCount:   len(goroutines),
		StateCounts:      domain.CountGoroutineStates(goroutines),
		TopOrigins:       topOrigins(goroutines, topOriginsLimit),
		Leaks:            domain.DetectLeaks(history.samples, goroutines, us.leakPolicy),
		LeakCheckStatus:  leakCheckStatus,
		GoroutineHistory: slices.Clone(history.totals),
		LeakedGoroutines: leakedIDs(goroutines),
	}
	us.inMemoryKVS.Set(latestSnapshotKey, snapshot)
	return RecordRuntimeSnapshotUsecaseOutput{Snapshot: snapshot}, nil
}

// carryOverLeaked は以前のリーク検査で到達不能と判定された G に、検査を飛ばした回でも Leaked を立てる。
// 到達不能になった G は二度と動かないので、一度判定されたら G が消えるまでリークとして扱ってよい。
func (us *RecordRuntimeSnapshotUsecase) carryOverLeaked(gs []domain.Goroutine) []domain.Goroutine {
	known := getOr(us.inMemoryKVS, leakedGoroutinesKey, map[domain.GoroutineID]struct{}{})
	leaked := make(map[domain.GoroutineID]struct{}, len(known))
	for i := range gs {
		if _, ok := known[gs[i].ID]; ok {
			gs[i].Leaked = true
		}
		if gs[i].Leaked {
			leaked[gs[i].ID] = struct{}{}
		}
	}
	us.inMemoryKVS.Set(leakedGoroutinesKey, leaked)
	return gs
}

func (us *RecordRuntimeSnapshotUsecase) fetchGoroutines(ctx context.Context, status LeakCheckStatus) ([]domain.Goroutine, LeakCheckStatus, error) {
	if status != LeakCheckDone {
		gs, err := us.runtimeClient.FetchGoroutines(ctx)
		return gs, status, err
	}
	gs, err := us.runtimeClient.FetchGoroutinesWithLeakCheck(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		us.inMemoryKVS.Set(leakCheckSupportedKey, false)
		gs, err = us.runtimeClient.FetchGoroutines(ctx)
		return gs, LeakCheckUnsupported, err
	}
	return gs, status, err
}

func (us *RecordRuntimeSnapshotUsecase) leakCheckStatusFor(dumpCount int) LeakCheckStatus {
	switch {
	case us.leakCheckEvery <= 0:
		return LeakCheckDisabled
	case !getOr(us.inMemoryKVS, leakCheckSupportedKey, true):
		return LeakCheckUnsupported
	case (dumpCount-1)%us.leakCheckEvery == 0:
		return LeakCheckDone
	default:
		return LeakCheckSkipped
	}
}

func leakedIDs(gs []domain.Goroutine) []domain.GoroutineID {
	var ids []domain.GoroutineID
	for _, g := range gs {
		if g.Leaked {
			ids = append(ids, g.ID)
		}
	}
	return ids
}

func topOrigins(gs []domain.Goroutine, limit int) []RuntimeSnapshotOriginCount {
	counts := map[domain.GoroutineOrigin]int{}
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

func getOr[T any](kvs domain.InMemoryKVS, key string, fallback T) T {
	v, ok := kvs.Get(key)
	if !ok {
		return fallback
	}
	t, ok := v.(T)
	if !ok {
		return fallback
	}
	return t
}

func appendBounded[T any](s []T, v T, size int) []T {
	s = append(s, v)
	if len(s) > size {
		s = s[len(s)-size:]
	}
	return s
}
