package usecase

import (
	"context"
	"errors"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type ExportObservationsUsecase struct {
	cursors   domain.CursorStore
	lifecycle domain.InstanceLifecycle
	buffer    domain.ObservationBuffer
	server    domain.ServerClient
}

func (us *UseCase) NewExportObservationsUsecase() *ExportObservationsUsecase {
	return &ExportObservationsUsecase{cursors: us.cursors, lifecycle: us.lifecycle, buffer: us.buffer, server: us.server}
}

type ExportObservationsUsecaseInput struct {
	Agent observation.AgentID
}

// ExportObservationsUsecaseOutput の Settled は、送り切って外した終わったインスタンス
type ExportObservationsUsecaseOutput struct {
	Sent    int
	Settled []observation.Instance
}

// Exec はインスタンスごとに、server が持っている位置の続きを送り、返ってきた位置を覚えて、server が持った分を捨てる。
// 1 つのインスタンスで失敗しても、ほかのインスタンスは続ける
func (u *ExportObservationsUsecase) Exec(ctx context.Context, in ExportObservationsUsecaseInput) (ExportObservationsUsecaseOutput, error) {
	buffered, err := u.buffer.Instances(ctx)
	if err != nil {
		return ExportObservationsUsecaseOutput{}, err
	}
	ended, err := u.lifecycle.EndedInstances(ctx)
	if err != nil {
		return ExportObservationsUsecaseOutput{}, err
	}
	endedSet := make(map[observation.Instance]bool, len(ended))
	for _, e := range ended {
		endedSet[e] = true
	}
	var (
		out  ExportObservationsUsecaseOutput
		errs []error
	)
	for _, instance := range domain.ExportOrder(buffered, ended) {
		settled, sent, err := u.export(ctx, in.Agent, instance, endedSet[instance])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if sent {
			out.Sent++
		}
		if settled {
			out.Settled = append(out.Settled, instance)
		}
	}
	return out, errors.Join(errs...)
}

func (u *ExportObservationsUsecase) export(ctx context.Context, agent observation.AgentID, instance observation.Instance, ended bool) (settled, sent bool, err error) {
	after, err := u.cursors.Cursor(ctx, instance)
	if err != nil {
		return false, false, err
	}
	buf, err := u.buffer.Buffered(ctx, instance)
	if err != nil {
		return false, false, err
	}
	batch := domain.BuildBatch(instance, after, agent, buf, ended)
	if batch.Empty() {
		return false, false, nil
	}
	cursors, err := u.server.Export(ctx, []observation.Batch{batch})
	if err != nil {
		return false, false, err
	}
	cursor, ok := cursors[instance]
	if !ok {
		return false, true, domain.ErrInternal.With("the server returned no cursor for " + string(instance.Name))
	}
	if err := u.cursors.SetCursor(ctx, instance, cursor); err != nil {
		return false, true, err
	}
	rest, err := u.buffer.DropCovered(ctx, instance, cursor, agent)
	if err != nil {
		return false, true, err
	}
	if !domain.Settled(ended, rest) {
		return false, true, nil
	}
	if err := u.buffer.Remove(ctx, instance); err != nil {
		return false, true, err
	}
	if err := u.lifecycle.Forget(ctx, instance); err != nil {
		return false, true, err
	}
	return true, true, nil
}
