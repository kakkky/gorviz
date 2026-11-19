package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// ListLogsSinceUsecase は取り込んだログのうち After より新しいものを、古い順に Limit 件まで返す。server へ送るのに使う
type ListLogsSinceUsecase struct {
	logRecording domain.LogRecording
}

func (us *UseCase) NewListLogsSinceUsecase() *ListLogsSinceUsecase {
	return &ListLogsSinceUsecase{logRecording: us.logRecording}
}

type ListLogsSinceUsecaseInput struct {
	After model.LogLineID
	Limit int
}

type ListLogsSinceUsecaseOutput struct {
	Logs []model.LogLine
}

func (us *ListLogsSinceUsecase) Exec(_ context.Context, in ListLogsSinceUsecaseInput) (ListLogsSinceUsecaseOutput, error) {
	var out []model.LogLine
	for _, l := range us.logRecording.Snapshot() {
		if l.ID <= in.After {
			continue
		}
		out = append(out, l)
		if len(out) >= in.Limit {
			break
		}
	}
	return ListLogsSinceUsecaseOutput{Logs: out}, nil
}
