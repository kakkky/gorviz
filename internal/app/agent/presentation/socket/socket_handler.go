package socket

import (
	"context"
	"fmt"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// InstructionHandler は server からの指示 1 種類を受け、返事に載せるものを reply に書く
type InstructionHandler interface {
	Handle(ctx context.Context, in agent.Instruction, reply *domain.Reply) error
}

// SocketHandler は server からの指示を、種類ごとの handler に渡す
type SocketHandler struct {
	handlers    map[agent.Op]InstructionHandler
	finish      *FinishHandler
	abandon     *usecase.AbandonAppObservationUsecase
	replySender domain.ServerSender
}

func NewSocketHandler(uc *usecase.UseCase, replySender domain.ServerSender) *SocketHandler {
	return &SocketHandler{
		handlers: map[agent.Op]InstructionHandler{
			agent.OpWriteTraceEvent:    NewWriteTraceEventHandler(uc.NewWriteAppRuntimeTraceEventUsecase()),
			agent.OpResetCoverage:      NewResetCoverageHandler(uc.NewResetAppCoverageUsecase()),
			agent.OpWriteCoverage:      NewWriteCoverageHandler(uc.NewReadAppCoverageUsecase()),
			agent.OpWriteGoroutineLeak: NewWriteGoroutineLeakHandler(uc.NewSendAppGoroutineLeaksUsecase()),
			agent.OpWriteThreadCount:   NewWriteThreadCountHandler(uc.NewWriteAppThreadCountUsecase()),
		},
		finish:      NewFinishHandler(uc.NewFinishAppObservationUsecase()),
		abandon:     uc.NewAbandonAppObservationUsecase(),
		replySender: replySender,
	}
}

// HandleMessage は 1 つの指示の失敗や panic で接続を止めない。返事にエラーを載せて返す
func (h *SocketHandler) HandleMessage(m agent.Message) {
	if m.Kind != agent.KindInstruction {
		return
	}
	in, err := agent.Decode[agent.Instruction](m)
	if err != nil {
		return
	}
	ctx := context.Background()
	// finish は接続を閉じるので、返事を返さない
	if in.Op == agent.OpFinish {
		_ = h.run(func() error { return h.finish.Handle(ctx) })
		return
	}
	reply := domain.Reply{ID: in.ID}
	err = h.run(func() error {
		handler, ok := h.handlers[in.Op]
		if !ok {
			return domain.ErrInvalidArgument.With("unknown instruction " + string(in.Op))
		}
		return handler.Handle(ctx, in, &reply)
	})
	if err != nil {
		reply.Error = err.Error()
	}
	_ = h.replySender.SendReply(reply)
}

func (h *SocketHandler) HandleDisconnect() {
	_ = h.run(func() error {
		_, err := h.abandon.Exec(context.Background(), usecase.AbandonAppObservationUsecaseInput{})
		return err
	})
}

func (h *SocketHandler) run(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = domain.ErrInternal.With(fmt.Sprint("panic: ", r))
		}
	}()
	return f()
}
