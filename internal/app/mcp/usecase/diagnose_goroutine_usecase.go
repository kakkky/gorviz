package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/mcp/domain"
	"github.com/kakkky/gorviz/internal/app/protocol/serverapi"
)

// DiagnoseGoroutineUsecase は G を At の前後で診断する。From を渡すと [From, At] の区間を見る
type DiagnoseGoroutineUsecase struct {
	serverFor domain.ServerFor
}

func (us *UseCase) NewDiagnoseGoroutineUsecase() *DiagnoseGoroutineUsecase {
	return &DiagnoseGoroutineUsecase{serverFor: us.serverFor}
}

type DiagnoseGoroutineUsecaseInput struct {
	Agent     string
	Goroutine int64
	At        time.Time
	From      time.Time
}

type DiagnoseGoroutineUsecaseOutput struct {
	Goroutine serverapi.GoroutineJSON
}

func (us *DiagnoseGoroutineUsecase) Exec(ctx context.Context, in DiagnoseGoroutineUsecaseInput) (DiagnoseGoroutineUsecaseOutput, error) {
	g, err := us.serverFor(in.Agent).Goroutine(ctx, in.Goroutine, in.At, in.From)
	if err != nil {
		return DiagnoseGoroutineUsecaseOutput{}, err
	}
	return DiagnoseGoroutineUsecaseOutput{Goroutine: g}, nil
}
