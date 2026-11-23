// Package rpc は agent から届いた観測データを受ける gRPC の口 (ObservationService の実装)
package rpc

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
)

type ObservationService struct {
	agentrpc.UnimplementedObservationServiceServer
	ingest *usecase.IngestObservationsUsecase
}

func NewObservationService(ingest *usecase.IngestObservationsUsecase) *ObservationService {
	return &ObservationService{ingest: ingest}
}

func (s *ObservationService) Export(ctx context.Context, req *agentrpc.ExportRequest) (*agentrpc.ExportResponse, error) {
	out, err := s.ingest.Exec(ctx, usecase.IngestObservationsUsecaseInput{Batches: agentrpc.FromExportRequest(req), Now: time.Now()})
	if err != nil {
		return nil, err
	}
	return agentrpc.ToExportResponse(out.Cursors), nil
}
