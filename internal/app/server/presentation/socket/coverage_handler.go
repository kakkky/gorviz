package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

type CoverageHandler struct {
	usecase *usecase.IngestAppObservationsUsecase
}

func NewCoverageHandler(uc *usecase.IngestAppObservationsUsecase) *CoverageHandler {
	return &CoverageHandler{usecase: uc}
}

func (h *CoverageHandler) HandleMessage(ctx context.Context, m agent.Message) error {
	c, err := agent.Decode[agent.Coverage](m)
	if err != nil {
		return domain.ErrInvalidArgument.Wrap(err, "decode coverage")
	}
	files := make([]domain.CoverageFile, 0, len(c.Files))
	for _, f := range c.Files {
		files = append(files, domain.CoverageFile{Name: f.Name, Data: f.Data})
	}
	_, err = h.usecase.Exec(ctx, usecase.IngestAppObservationsUsecaseInput{Kind: usecase.ObservationCoverage, Coverage: files})
	return err
}
