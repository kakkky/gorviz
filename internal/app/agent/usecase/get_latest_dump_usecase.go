package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// GetLatestDumpUsecase は最後に取った goroutine dump を返す。server へ送るのに使う
type GetLatestDumpUsecase struct {
	inMemoryKVS domain.InMemoryKVS
}

func (us *UseCase) NewGetLatestDumpUsecase() *GetLatestDumpUsecase {
	return &GetLatestDumpUsecase{inMemoryKVS: us.inMemoryKVS}
}

type GetLatestDumpUsecaseInput struct{}

type GetLatestDumpUsecaseOutput struct {
	Dump model.GoroutineDump
	OK   bool
}

func (us *GetLatestDumpUsecase) Exec(_ context.Context, _ GetLatestDumpUsecaseInput) (GetLatestDumpUsecaseOutput, error) {
	d, ok := domain.KVSGet[model.GoroutineDump](us.inMemoryKVS, latestDumpKey)
	return GetLatestDumpUsecaseOutput{Dump: d, OK: ok}, nil
}
