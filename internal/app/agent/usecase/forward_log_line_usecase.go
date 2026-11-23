package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type ForwardLogLineUsecase struct {
	targets   domain.TargetRegistry
	sequencer domain.LogSequencer
	buffer    domain.ObservationBuffer
}

func (us *UseCase) NewForwardLogLineUsecase() *ForwardLogLineUsecase {
	return &ForwardLogLineUsecase{targets: us.targets, sequencer: us.sequencer, buffer: us.buffer}
}

type ForwardLogLineUsecaseInput struct {
	Agent        observation.AgentID
	Source       observation.LogSource
	Path         string
	Raw          string
	ReceivedAt   time.Time
	DurationKeys []string
}

// ForwardLogLineUsecaseOutput の Dropped は、どのアプリのログか決まらず捨てたこと
type ForwardLogLineUsecaseOutput struct {
	Instance observation.Instance
	Dropped  bool
}

func (u *ForwardLogLineUsecase) Exec(ctx context.Context, in ForwardLogLineUsecaseInput) (ForwardLogLineUsecaseOutput, error) {
	targets, err := u.targets.Targets(ctx)
	if err != nil {
		return ForwardLogLineUsecaseOutput{}, err
	}
	target, ok := domain.ResolveLogTarget(targets, in.Source, in.Path)
	if !ok {
		return ForwardLogLineUsecaseOutput{Dropped: true}, nil
	}
	line := domain.ParseLogLine(in.Raw, in.Source, in.ReceivedAt, in.DurationKeys)
	seq, err := u.sequencer.NextLogSeq(ctx, target.Instance)
	if err != nil {
		return ForwardLogLineUsecaseOutput{}, err
	}
	line.Agent, line.Seq = in.Agent, seq
	if err := u.buffer.PushLogLine(ctx, target.Instance, line); err != nil {
		return ForwardLogLineUsecaseOutput{}, err
	}
	return ForwardLogLineUsecaseOutput{Instance: target.Instance}, nil
}
