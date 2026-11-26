package socket

import (
	"context"
	"fmt"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// SocketHandler は server からの指示を、種類ごとに 1 つの usecase に渡す
type SocketHandler struct {
	writeEvent  *usecase.WriteAppRuntimeTraceEventUsecase
	reset       *usecase.ResetAppCoverageUsecase
	coverage    *usecase.ReadAppCoverageUsecase
	leaks       *usecase.SendAppGoroutineLeaksUsecase
	threads     *usecase.WriteAppThreadCountUsecase
	finish      *usecase.FinishAppObservationUsecase
	abandon     *usecase.AbandonAppObservationUsecase
	replySender domain.ServerSender
}

func NewSocketHandler(
	writeEvent *usecase.WriteAppRuntimeTraceEventUsecase,
	reset *usecase.ResetAppCoverageUsecase,
	coverage *usecase.ReadAppCoverageUsecase,
	leaks *usecase.SendAppGoroutineLeaksUsecase,
	threads *usecase.WriteAppThreadCountUsecase,
	finish *usecase.FinishAppObservationUsecase,
	abandon *usecase.AbandonAppObservationUsecase,
	replySender domain.ServerSender,
) *SocketHandler {
	return &SocketHandler{writeEvent: writeEvent, reset: reset, coverage: coverage, leaks: leaks, threads: threads, finish: finish, abandon: abandon, replySender: replySender}
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
	if in.Op == agent.OpFinish {
		h.run(func() error {
			_, err := h.finish.Exec(context.Background(), usecase.FinishAppObservationUsecaseInput{})
			return err
		})
		return
	}
	reply := domain.Reply{ID: in.ID}
	err = h.run(func() error { return h.instruct(in, &reply) })
	if err != nil {
		reply.Error = err.Error()
	}
	_ = h.replySender.SendReply(reply)
}

func (h *SocketHandler) instruct(in agent.Instruction, reply *domain.Reply) error {
	ctx := context.Background()
	switch in.Op {
	case agent.OpWriteTraceEvent:
		_, err := h.writeEvent.Exec(ctx, usecase.WriteAppRuntimeTraceEventUsecaseInput{Event: in.Event})
		return err
	case agent.OpResetCoverage:
		_, err := h.reset.Exec(ctx, usecase.ResetAppCoverageUsecaseInput{})
		return err
	case agent.OpWriteCoverage:
		out, err := h.coverage.Exec(ctx, usecase.ReadAppCoverageUsecaseInput{})
		reply.Coverage = out.Files
		return err
	case agent.OpWriteGoroutineLeak:
		_, err := h.leaks.Exec(ctx, usecase.SendAppGoroutineLeaksUsecaseInput{})
		return err
	case agent.OpWriteThreadCount:
		_, err := h.threads.Exec(ctx, usecase.WriteAppThreadCountUsecaseInput{})
		return err
	}
	return domain.ErrInvalidArgument.With("unknown instruction " + string(in.Op))
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
