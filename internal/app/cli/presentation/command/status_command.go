package command

import (
	"context"
	"net/http"

	"github.com/urfave/cli/v3"
)

func (c *Command) statusCommand() *cli.Command {
	return &cli.Command{Name: "status", Usage: "show whether the app runs, the GUI URL and the diagnosed ranges", Action: c.status}
}

func (c *Command) status(_ context.Context, cmd *cli.Command) error {
	return c.print(cmd, http.MethodGet, statusEndpoint, nil, nil)
}
