package socket

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agentsocket"
)

// SocketHandler は agent から届いた観測データを、種類ごとに取り込ませる。1 つのメッセージの失敗や panic で、ほかのメッセージを読めなくしない
type SocketHandler struct {
	ingest *usecase.IngestAppObservationsUsecase
}

func NewSocketHandler(ingest *usecase.IngestAppObservationsUsecase) *SocketHandler {
	return &SocketHandler{ingest: ingest}
}

func (h *SocketHandler) HandleMessage(ctx context.Context, m agentsocket.Message) {
	h.run(ctx, m.Kind, func() error {
		in, ok, err := inputOf(m)
		if err != nil || !ok {
			return err
		}
		_, err = h.ingest.Exec(ctx, in)
		return err
	})
}

func (h *SocketHandler) HandleDisconnect(ctx context.Context, clean bool) {
	h.run(ctx, 0, func() error {
		_, err := h.ingest.Exec(ctx, usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationEnd, Broken: !clean})
		return err
	})
}

func inputOf(m agentsocket.Message) (usecase.IngestAppObservationsUsecaseInput, bool, error) {
	switch m.Kind {
	case agentsocket.KindHello:
		h, err := agentsocket.Decode[agentsocket.Hello](m)
		if err != nil {
			return usecase.IngestAppObservationsUsecaseInput{}, false, domain.ErrInvalidArgument.Wrap(err, "decode hello")
		}
		return usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationHello, Hello: domain.AppRun{GoVersion: h.GoVersion, Procs: h.Procs, PID: h.PID}}, true, nil
	case agentsocket.KindTrace:
		return usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationTrace, Trace: m.Payload}, true, nil
	case agentsocket.KindGoroutineLeakProfile:
		p, err := agentsocket.Decode[agentsocket.GoroutineLeakProfile](m)
		if err != nil {
			return usecase.IngestAppObservationsUsecaseInput{}, false, domain.ErrInvalidArgument.Wrap(err, "decode goroutineleak profile")
		}
		return usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationGoroutineLeakProfile, LeakSeq: p.Seq, LeakText: p.Text}, true, nil
	case agentsocket.KindCoverage:
		c, err := agentsocket.Decode[agentsocket.Coverage](m)
		if err != nil {
			return usecase.IngestAppObservationsUsecaseInput{}, false, domain.ErrInvalidArgument.Wrap(err, "decode coverage")
		}
		files := make([]domain.CoverageFile, 0, len(c.Files))
		for _, f := range c.Files {
			files = append(files, domain.CoverageFile{Name: f.Name, Data: f.Data})
		}
		return usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationCoverage, Coverage: files}, true, nil
	}
	return usecase.IngestAppObservationsUsecaseInput{}, false, nil
}

func (h *SocketHandler) run(ctx context.Context, kind agentsocket.Kind, f func() error) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "panic while handling an agent message", slog.Int("kind", int(kind)), slog.String("panic", fmt.Sprint(r)))
		}
	}()
	if err := f(); err != nil {
		slog.ErrorContext(ctx, "handle an agent message", slog.Int("kind", int(kind)), slog.Any("err", err))
	}
}
