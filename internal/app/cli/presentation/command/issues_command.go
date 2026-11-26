package command

import (
	"context"
	"net/http"
	"net/url"

	"github.com/urfave/cli/v3"

	"github.com/kakkky/gorviz/internal/protocol/server"
)

func (c *Command) issues(_ context.Context, cmd *cli.Command) error {
	q := url.Values{}
	for _, f := range []string{"kind", "text", "after"} {
		if v := cmd.String(f); v != "" {
			q.Set(f, v)
		}
	}
	return c.print(cmd, http.MethodGet, server.IssuesEndpoint, q, nil)
}
