// Package ingest は server の側で、push の接続の上で agent からの呼び出しを受ける
package ingest

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/api/agentrpc"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// Service は net/rpc の受け手。メソッド名がそのまま agent から呼ぶ名前になる
type Service struct {
	receiveAgentPushUsecase *usecase.ReceiveAgentPushUsecase
}

func NewService(uc *usecase.UseCase) *Service {
	return &Service{receiveAgentPushUsecase: uc.NewReceiveAgentPushUsecase()}
}

func (s *Service) Push(a agentrpc.PushArgs, _ *agentrpc.Empty) error {
	_, err := s.receiveAgentPushUsecase.Exec(context.Background(), usecase.ReceiveAgentPushUsecaseInput{Logs: a.Logs, Dump: a.Dump})
	return err
}
