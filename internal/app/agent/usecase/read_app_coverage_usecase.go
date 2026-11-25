package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

// ReadAppCoverageUsecase はアプリの coverage を書き出して返す。返事に載せて送るのは presentation
type ReadAppCoverageUsecase struct {
	coverage domain.AppCoverage
}

type ReadAppCoverageUsecaseInput struct{}

type ReadAppCoverageUsecaseOutput struct {
	Files []domain.CoverageFile
}

func (us *UseCase) NewReadAppCoverageUsecase() *ReadAppCoverageUsecase {
	return &ReadAppCoverageUsecase{coverage: us.coverage}
}

func (u *ReadAppCoverageUsecase) Exec(_ context.Context, _ ReadAppCoverageUsecaseInput) (ReadAppCoverageUsecaseOutput, error) {
	files, err := u.coverage.ReadCoverage()
	if err != nil {
		return ReadAppCoverageUsecaseOutput{}, err
	}
	return ReadAppCoverageUsecaseOutput{Files: files}, nil
}
