// Package pull は agent の側で、pull の接続の上で server からの trace の要求に答える。持っているものを返すだけで、判定はしない
package pull

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/app/protocol/agentrpc"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// Service は agentrpc.AgentServer の実装
type Service struct {
	agentrpc.UnimplementedAgentServer
	getTraceChunkUsecase    *usecase.GetTraceChunkUsecase
	listTraceChunksUsecase  *usecase.ListTraceChunksUsecase
	findLogMarkUsecase      *usecase.FindLogMarkUsecase
	getRecordingSpanUsecase *usecase.GetRecordingSpanUsecase
}

func NewService(uc *usecase.UseCase) *Service {
	return &Service{
		getTraceChunkUsecase:    uc.NewGetTraceChunkUsecase(),
		listTraceChunksUsecase:  uc.NewListTraceChunksUsecase(),
		findLogMarkUsecase:      uc.NewFindLogMarkUsecase(),
		getRecordingSpanUsecase: uc.NewGetRecordingSpanUsecase(),
	}
}

func (s *Service) LatestChunk(ctx context.Context, req *agentrpc.LatestChunkRequest) (*agentrpc.ChunkReply, error) {
	out, err := s.getTraceChunkUsecase.Exec(ctx, usecase.GetTraceChunkUsecaseInput{})
	if err != nil || !out.OK {
		return &agentrpc.ChunkReply{}, err
	}
	if since := agentrpc.TimeOf(req.GetIfNewerThan()); !since.IsZero() && !out.Chunk.StartedAt.After(since) {
		return &agentrpc.ChunkReply{NotModified: true}, nil
	}
	return &agentrpc.ChunkReply{Chunk: withoutRaw(out.Chunk)}, nil
}

func (s *Service) ChunkAt(ctx context.Context, req *agentrpc.ChunkAtRequest) (*agentrpc.ChunkReply, error) {
	out, err := s.getTraceChunkUsecase.Exec(ctx, usecase.GetTraceChunkUsecaseInput{At: agentrpc.TimeOf(req.GetAt())})
	if err != nil || !out.OK {
		return &agentrpc.ChunkReply{}, err
	}
	return &agentrpc.ChunkReply{Chunk: withoutRaw(out.Chunk)}, nil
}

func (s *Service) ChunksBetween(req *agentrpc.ChunksBetweenRequest, stream grpc.ServerStreamingServer[agentrpc.TraceChunk]) error {
	out, err := s.listTraceChunksUsecase.Exec(stream.Context(), usecase.ListTraceChunksUsecaseInput{From: agentrpc.TimeOf(req.GetFrom()), To: agentrpc.TimeOf(req.GetTo())})
	if err != nil {
		return err
	}
	for _, c := range out.Chunks {
		if err := stream.Send(agentrpc.NewTraceChunk(c)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) FindLogMark(ctx context.Context, req *agentrpc.FindLogMarkRequest) (*agentrpc.FindLogMarkReply, error) {
	out, err := s.findLogMarkUsecase.Exec(ctx, usecase.FindLogMarkUsecaseInput{Seq: req.GetSeq()})
	if err != nil || !out.OK {
		return &agentrpc.FindLogMarkReply{}, err
	}
	return &agentrpc.FindLogMarkReply{Chunk: withoutRaw(out.Chunk), Mark: agentrpc.NewTraceLogMark(out.Mark)}, nil
}

func (s *Service) Span(ctx context.Context, _ *emptypb.Empty) (*agentrpc.SpanReply, error) {
	out, err := s.getRecordingSpanUsecase.Exec(ctx, usecase.GetRecordingSpanUsecaseInput{})
	if err != nil || !out.OK {
		return &agentrpc.SpanReply{}, err
	}
	return &agentrpc.SpanReply{Span: agentrpc.NewRecordingSpan(out.Span)}, nil
}

// withoutRaw は生の trace を落として送る形にする。Live の表示には要らず、運ぶ量が大きいので、保存するとき (ChunksBetween) だけ運ぶ
func withoutRaw(c model.TraceChunk) *agentrpc.TraceChunk {
	c.Raw = nil
	return agentrpc.NewTraceChunk(c)
}
