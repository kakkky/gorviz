package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/mcp/domain"
)

// ListRecordingsUsecase は server に保存した録画の一覧を取る
type ListRecordingsUsecase struct {
	serverFor domain.ServerFor
}

func (us *UseCase) NewListRecordingsUsecase() *ListRecordingsUsecase {
	return &ListRecordingsUsecase{serverFor: us.serverFor}
}

type ListRecordingsUsecaseInput struct {
	Agent string
}

type ListRecordingsUsecaseOutput struct {
	Recordings []model.SavedRecording
}

func (us *ListRecordingsUsecase) Exec(ctx context.Context, in ListRecordingsUsecaseInput) (ListRecordingsUsecaseOutput, error) {
	recs, err := us.serverFor(in.Agent).Recordings(ctx)
	if err != nil {
		return ListRecordingsUsecaseOutput{}, err
	}
	return ListRecordingsUsecaseOutput{Recordings: recs}, nil
}
