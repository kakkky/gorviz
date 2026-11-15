package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// ReceiveAgentPushUsecase は agent が送ってきた軽いもの (ログ・dump) を受け取る。判定は refresh ごとに別に回す
type ReceiveAgentPushUsecase struct {
	agentInbox domain.AgentInbox
}

func (us *UseCase) NewReceiveAgentPushUsecase() *ReceiveAgentPushUsecase {
	return &ReceiveAgentPushUsecase{agentInbox: us.agentInbox}
}

// ReceiveAgentPushUsecaseInput の Dump は、前に送ったものから変わっていなければ nil
type ReceiveAgentPushUsecaseInput struct {
	Logs []model.LogLine
	Dump *model.GoroutineDump
}

type ReceiveAgentPushUsecaseOutput struct{}

func (us *ReceiveAgentPushUsecase) Exec(_ context.Context, in ReceiveAgentPushUsecaseInput) (ReceiveAgentPushUsecaseOutput, error) {
	us.agentInbox.Receive(in.Logs, in.Dump)
	return ReceiveAgentPushUsecaseOutput{}, nil
}
