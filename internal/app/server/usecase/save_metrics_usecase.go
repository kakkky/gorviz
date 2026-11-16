package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// SaveMetricsUsecase は閉じた区切りの集計を保存し、保持期間を過ぎたものを捨てる
type SaveMetricsUsecase struct {
	metricStore domain.MetricStore
}

func (us *UseCase) NewSaveMetricsUsecase() *SaveMetricsUsecase {
	return &SaveMetricsUsecase{metricStore: us.metricStore}
}

type SaveMetricsUsecaseInput struct {
	Now time.Time
}

type SaveMetricsUsecaseOutput struct{}

func (us *SaveMetricsUsecase) Exec(_ context.Context, in SaveMetricsUsecaseInput) (SaveMetricsUsecaseOutput, error) {
	return SaveMetricsUsecaseOutput{}, us.metricStore.Flush(in.Now)
}
