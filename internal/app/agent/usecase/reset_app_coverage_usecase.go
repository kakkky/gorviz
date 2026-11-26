package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// ResetAppCoverageUsecase はアプリの coverage のカウンタを 0 にする
type ResetAppCoverageUsecase struct {
	coverage domain.AppRuntimeCoverage
}

type ResetAppCoverageUsecaseInput struct{}

type ResetAppCoverageUsecaseOutput struct{}

func (us *UseCase) NewResetAppCoverageUsecase() *ResetAppCoverageUsecase {
	return &ResetAppCoverageUsecase{coverage: us.coverage}
}

func (u *ResetAppCoverageUsecase) Exec(_ context.Context, _ ResetAppCoverageUsecaseInput) (ResetAppCoverageUsecaseOutput, error) {
	if err := u.coverage.ResetCoverage(); err != nil {
		return ResetAppCoverageUsecaseOutput{}, err
	}
	return ResetAppCoverageUsecaseOutput{}, nil
}
