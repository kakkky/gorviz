package usecase

import (
	"context"
	"errors"
	"io"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

const traceReadSize = 256 << 10

// StreamAppRuntimeTraceUsecase は流れてきた execution trace を 1 回分読み、server へ送る。
// 送れなくても読むのはやめない。読まないと trace.Stop が戻らず、runtime が trace を溜め続けるため
type StreamAppRuntimeTraceUsecase struct {
	trace  domain.AppRuntimeTrace
	sender domain.ServerSender
	buf    []byte
}

type StreamAppRuntimeTraceUsecaseInput struct{}

type StreamAppRuntimeTraceUsecaseOutput struct {
	Ended bool
}

func (us *UseCase) NewStreamAppRuntimeTraceUsecase() *StreamAppRuntimeTraceUsecase {
	return &StreamAppRuntimeTraceUsecase{trace: us.trace, sender: us.sender, buf: make([]byte, traceReadSize)}
}

func (u *StreamAppRuntimeTraceUsecase) Exec(_ context.Context, _ StreamAppRuntimeTraceUsecaseInput) (StreamAppRuntimeTraceUsecaseOutput, error) {
	n, err := u.trace.ReadTrace(u.buf)
	if n > 0 {
		if sendErr := u.sender.SendTrace(u.buf[:n]); sendErr != nil && err == nil {
			return StreamAppRuntimeTraceUsecaseOutput{}, sendErr
		}
	}
	if errors.Is(err, io.EOF) {
		return StreamAppRuntimeTraceUsecaseOutput{Ended: true}, nil
	}
	if err != nil {
		return StreamAppRuntimeTraceUsecaseOutput{}, domain.ErrInternal.Wrap(err, "read execution trace")
	}
	return StreamAppRuntimeTraceUsecaseOutput{}, nil
}
