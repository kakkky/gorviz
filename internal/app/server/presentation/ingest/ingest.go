// Package ingest は server の側で、push の接続の上で agent からの呼び出しを受ける
package ingest

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/kakkky/gorviz/internal/app/protocol/agentrpc"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// Service は agentrpc.IngestServer の実装
type Service struct {
	agentrpc.UnimplementedIngestServer
	receiveAgentPushUsecase *usecase.ReceiveAgentPushUsecase
}

func NewService(uc *usecase.UseCase) *Service {
	return &Service{receiveAgentPushUsecase: uc.NewReceiveAgentPushUsecase()}
}

func (s *Service) Push(ctx context.Context, req *agentrpc.PushRequest) (*emptypb.Empty, error) {
	in := usecase.ReceiveAgentPushUsecaseInput{}
	for _, l := range req.GetLogs() {
		in.Logs = append(in.Logs, l.Model())
	}
	if d := req.GetDump(); d != nil {
		dump := d.Model()
		in.Dump = &dump
	}
	_, err := s.receiveAgentPushUsecase.Exec(ctx, in)
	return &emptypb.Empty{}, err
}
