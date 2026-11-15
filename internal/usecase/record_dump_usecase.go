package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/kakkky/gorviz/internal/domain"
)

const (
	dumpCountKey          = "dump:count"
	leakCheckSupportedKey = "dump:leak-check-supported"
	leakedGoroutinesKey   = "dump:leaked-goroutines"
	latestDumpKey         = "dump:latest"
)

// RecordDumpUsecase は agent の側で goroutine dump を取って持つ。判定 (リークの推移など) はしない
type RecordDumpUsecase struct {
	runtimeClient  domain.RuntimeClient
	inMemoryKVS    domain.InMemoryKVS
	leakCheckEvery int
}

func (us *UseCase) NewRecordDumpUsecase() *RecordDumpUsecase {
	return &RecordDumpUsecase{runtimeClient: us.runtimeClient, inMemoryKVS: us.inMemoryKVS, leakCheckEvery: us.cfg.LeakCheckEvery}
}

type RecordDumpUsecaseInput struct {
	Now time.Time
}

type RecordDumpUsecaseOutput struct {
	Dump domain.GoroutineDump
}

// Exec は dump を 1 回取る。LeakCheckEvery 回に 1 回は runtime のリーク判定つきで取る
func (us *RecordDumpUsecase) Exec(ctx context.Context, in RecordDumpUsecaseInput) (RecordDumpUsecaseOutput, error) {
	count := getOr(us.inMemoryKVS, dumpCountKey, 0) + 1
	us.inMemoryKVS.Set(dumpCountKey, count)
	gs, status, err := us.fetch(ctx, us.leakCheckStatusFor(count))
	if err != nil {
		return RecordDumpUsecaseOutput{}, err
	}
	dump := domain.GoroutineDump{At: in.Now, Goroutines: us.carryOverLeaked(gs), LeakCheck: status}
	us.inMemoryKVS.Set(latestDumpKey, dump)
	return RecordDumpUsecaseOutput{Dump: dump}, nil
}

// LatestDump は domain.DumpSource として、agent が最後に取った dump を返す
func (us *RecordDumpUsecase) LatestDump() (domain.GoroutineDump, bool) {
	return getAs[domain.GoroutineDump](us.inMemoryKVS, latestDumpKey)
}

// carryOverLeaked は以前のリーク検査で到達不能と判定された G に、検査を飛ばした回でも Leaked を立てる。
// 到達不能になった G は二度と動かないので、一度判定されたら G が消えるまでリークとして扱ってよい。
func (us *RecordDumpUsecase) carryOverLeaked(gs []domain.Goroutine) []domain.Goroutine {
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

func (us *RecordDumpUsecase) fetch(ctx context.Context, status domain.LeakCheckStatus) ([]domain.Goroutine, domain.LeakCheckStatus, error) {
	if status != domain.LeakCheckDone {
		gs, err := us.runtimeClient.FetchGoroutines(ctx)
		return gs, status, err
	}
	gs, err := us.runtimeClient.FetchGoroutinesWithLeakCheck(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		us.inMemoryKVS.Set(leakCheckSupportedKey, false)
		gs, err = us.runtimeClient.FetchGoroutines(ctx)
		return gs, domain.LeakCheckUnsupported, err
	}
	return gs, status, err
}

func (us *RecordDumpUsecase) leakCheckStatusFor(dumpCount int) domain.LeakCheckStatus {
	switch {
	case us.leakCheckEvery <= 0:
		return domain.LeakCheckDisabled
	case !getOr(us.inMemoryKVS, leakCheckSupportedKey, true):
		return domain.LeakCheckUnsupported
	case (dumpCount-1)%us.leakCheckEvery == 0:
		return domain.LeakCheckDone
	default:
		return domain.LeakCheckSkipped
	}
}
