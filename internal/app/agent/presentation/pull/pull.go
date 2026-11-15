// Package pull は agent の側で、pull の接続の上で server からの trace の要求に答える。持っているものを返すだけで、判定はしない
package pull

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/app/api/agentrpc"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

// Service は net/rpc の受け手。メソッド名がそのまま server から呼ぶ名前になる
type Service struct {
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

func (s *Service) LatestChunk(a agentrpc.ChunkArgs, r *agentrpc.ChunkReply) error {
	out, err := s.getTraceChunkUsecase.Exec(context.Background(), usecase.GetTraceChunkUsecaseInput{})
	if err != nil {
		return err
	}
	if out.OK && !a.IfNewerThan.IsZero() && !out.Chunk.StartedAt.After(a.IfNewerThan) {
		r.NotModified = true
		return nil
	}
	r.Chunk, r.OK = strip(out.Chunk, a.WithRaw), out.OK
	return nil
}

func (s *Service) ChunkAt(a agentrpc.ChunkArgs, r *agentrpc.ChunkReply) error {
	out, err := s.getTraceChunkUsecase.Exec(context.Background(), usecase.GetTraceChunkUsecaseInput{At: a.At})
	if err != nil {
		return err
	}
	r.Chunk, r.OK = strip(out.Chunk, a.WithRaw), out.OK
	return nil
}

func (s *Service) ChunksBetween(a agentrpc.BetweenArgs, r *agentrpc.ChunksReply) error {
	out, err := s.listTraceChunksUsecase.Exec(context.Background(), usecase.ListTraceChunksUsecaseInput{From: a.From, To: a.To})
	if err != nil {
		return err
	}
	for _, c := range out.Chunks {
		r.Chunks = append(r.Chunks, strip(c, a.WithRaw))
	}
	return nil
}

func (s *Service) FindLogMark(a agentrpc.SeqArgs, r *agentrpc.MarkReply) error {
	out, err := s.findLogMarkUsecase.Exec(context.Background(), usecase.FindLogMarkUsecaseInput{Seq: a.Seq})
	if err != nil {
		return err
	}
	r.Chunk, r.Mark, r.OK = strip(out.Chunk, false), out.Mark, out.OK
	return nil
}

func (s *Service) Span(_ agentrpc.Empty, r *agentrpc.SpanReply) error {
	out, err := s.getRecordingSpanUsecase.Exec(context.Background(), usecase.GetRecordingSpanUsecaseInput{})
	if err != nil {
		return err
	}
	r.Span, r.OK = out.Span, out.OK
	return nil
}

// strip は生の trace を落とす。Live の表示には要らず、運ぶ量が大きいので、保存するときだけ運ぶ
func strip(c model.TraceChunk, withRaw bool) model.TraceChunk {
	if !withRaw {
		c.Raw = nil
	}
	return c
}
