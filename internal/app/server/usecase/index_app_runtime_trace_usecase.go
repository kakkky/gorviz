package usecase

import (
	"context"
	"errors"
	"io"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// IndexAppRuntimeTraceUsecase は流れてくる execution trace を、trace が終わるまで読み続けて観測データの index に足す。
// 読み手は Exec の中で持ち、呼び出しをまたいで持たない
type IndexAppRuntimeTraceUsecase struct {
	index  domain.TraceIndexStore
	stream domain.TraceStream
}

type IndexAppRuntimeTraceUsecaseInput struct{}

type IndexAppRuntimeTraceUsecaseOutput struct {
	Broken bool
}

func (us *UseCase) NewIndexAppRuntimeTraceUsecase() *IndexAppRuntimeTraceUsecase {
	return &IndexAppRuntimeTraceUsecase{index: us.index, stream: us.stream}
}

func (u *IndexAppRuntimeTraceUsecase) Exec(_ context.Context, _ IndexAppRuntimeTraceUsecaseInput) (IndexAppRuntimeTraceUsecaseOutput, error) {
	dec, err := domain.NewTraceDecoder(u.stream.TraceReader())
	if err != nil {
		broken := u.stream.TraceBroken()
		u.index.UpdateTraceIndex(func(ix *domain.TraceIndex) { ix.End(broken) })
		return IndexAppRuntimeTraceUsecaseOutput{Broken: broken}, nil
	}
	for {
		ev, readErr := dec.Next()
		if readErr != nil {
			broken := !errors.Is(readErr, io.EOF) || u.stream.TraceBroken()
			u.index.UpdateTraceIndex(func(ix *domain.TraceIndex) { ix.End(broken) })
			return IndexAppRuntimeTraceUsecaseOutput{Broken: broken}, nil
		}
		u.index.UpdateTraceIndex(func(ix *domain.TraceIndex) { ix.Apply(ev) })
	}
}
