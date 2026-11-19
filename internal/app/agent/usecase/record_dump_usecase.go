package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
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
	return &RecordDumpUsecase{runtimeClient: us.runtimeClient, inMemoryKVS: us.inMemoryKVS, leakCheckEvery: us.settings.LeakCheckEvery}
}

type RecordDumpUsecaseInput struct {
	Now time.Time
}

type RecordDumpUsecaseOutput struct {
	Dump model.GoroutineDump
}

// Exec は dump を 1 回取る。LeakCheckEvery 回に 1 回は runtime のリーク判定つきで取る
func (us *RecordDumpUsecase) Exec(ctx context.Context, in RecordDumpUsecaseInput) (RecordDumpUsecaseOutput, error) {
	count := domain.KVSGetOr(us.inMemoryKVS, dumpCountKey, 0) + 1
	us.inMemoryKVS.Set(dumpCountKey, count)
	gs, status, err := us.fetch(ctx, us.leakCheckStatusFor(count))
	if err != nil {
		return RecordDumpUsecaseOutput{}, err
	}
	dump := model.GoroutineDump{At: in.Now, Goroutines: us.carryOverLeaked(gs), LeakCheck: status}
	us.inMemoryKVS.Set(latestDumpKey, dump)
	return RecordDumpUsecaseOutput{Dump: dump}, nil
}

// carryOverLeaked は以前のリーク検査で到達不能と判定された G に、検査を飛ばした回でも Leaked を立てる。
// 到達不能になった G は二度と動かないので、一度判定されたら G が消えるまでリークとして扱ってよい。
func (us *RecordDumpUsecase) carryOverLeaked(gs []model.Goroutine) []model.Goroutine {
	known := domain.KVSGetOr(us.inMemoryKVS, leakedGoroutinesKey, map[model.GoroutineID]struct{}{})
	leaked := make(map[model.GoroutineID]struct{}, len(known))
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

func (us *RecordDumpUsecase) fetch(ctx context.Context, status model.LeakCheckStatus) ([]model.Goroutine, model.LeakCheckStatus, error) {
	if status != model.LeakCheckDone {
		gs, err := us.runtimeClient.FetchGoroutines(ctx)
		return gs, status, err
	}
	gs, err := us.runtimeClient.FetchGoroutinesWithLeakCheck(ctx)
	if errors.Is(err, model.ErrNotFound) {
		us.inMemoryKVS.Set(leakCheckSupportedKey, false)
		gs, err = us.runtimeClient.FetchGoroutines(ctx)
		return gs, model.LeakCheckUnsupported, err
	}
	return gs, status, err
}

func (us *RecordDumpUsecase) leakCheckStatusFor(dumpCount int) model.LeakCheckStatus {
	switch {
	case us.leakCheckEvery <= 0:
		return model.LeakCheckDisabled
	case !domain.KVSGetOr(us.inMemoryKVS, leakCheckSupportedKey, true):
		return model.LeakCheckUnsupported
	case (dumpCount-1)%us.leakCheckEvery == 0:
		return model.LeakCheckDone
	default:
		return model.LeakCheckSkipped
	}
}
