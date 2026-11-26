package command

import (
	"context"
	"net/http"

	"github.com/urfave/cli/v3"

	"github.com/kakkky/gorviz/internal/protocol/server"
)

func (c *Command) status(_ context.Context, cmd *cli.Command) error {
	return c.print(cmd, http.MethodGet, server.StatusEndpoint, nil, nil)
}
