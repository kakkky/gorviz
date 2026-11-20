package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// ReceiveAgentPushUsecase は agent が送ってきたログ・dump・区切りを受け取る。判定は refresh ごとに別に回す
type ReceiveAgentPushUsecase struct {
	agentInbox     domain.AgentInbox
	traceRecording domain.TraceRecording
}

func (us *UseCase) NewReceiveAgentPushUsecase() *ReceiveAgentPushUsecase {
	return &ReceiveAgentPushUsecase{agentInbox: us.agentInbox, traceRecording: us.traceRecording}
}

// ReceiveAgentPushUsecaseInput の Dump は、前に送ったものから変わっていなければ nil。Chunks は古い順
type ReceiveAgentPushUsecaseInput struct {
	Logs   []model.LogLine
	Dump   *model.GoroutineDump
	Chunks []model.TraceChunk
}

type ReceiveAgentPushUsecaseOutput struct{}

func (us *ReceiveAgentPushUsecase) Exec(_ context.Context, in ReceiveAgentPushUsecaseInput) (ReceiveAgentPushUsecaseOutput, error) {
	us.agentInbox.Receive(in.Logs, in.Dump)
	for _, c := range in.Chunks {
		us.traceRecording.Append(c)
	}
	return ReceiveAgentPushUsecaseOutput{}, nil
}
