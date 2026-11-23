package client

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/grpcconn"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
)

// GorvizServerClient は観測データを server に送る。接続は grpcconn の 1 本を使う
type GorvizServerClient struct {
	rpc     agentrpc.ObservationServiceClient
	timeout time.Duration
}

func NewGorvizServerClient(cfg *config.Config, conn *grpcconn.ClientConn) (*GorvizServerClient, error) {
	return &GorvizServerClient{rpc: agentrpc.NewObservationServiceClient(conn), timeout: cfg.RequestTimeout}, nil
}

func (c *GorvizServerClient) Export(ctx context.Context, batches []observation.Batch) (map[observation.Instance]observation.Cursor, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	res, err := c.rpc.Export(ctx, agentrpc.ToExportRequest(batches))
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "export observations")
	}
	return agentrpc.FromExportResponse(res), nil
}
