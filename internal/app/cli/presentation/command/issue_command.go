package command

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/urfave/cli/v3"

	"github.com/kakkky/gorviz/internal/protocol/server"
)

func (c *Command) issue(_ context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return cli.Exit("gorviz issue needs an issue ID, like gorviz issue I1; list them with gorviz issues", 2)
	}
	q := url.Values{}
	if n := cmd.Int("occurrence"); n > 0 {
		q.Set("occurrence", fmt.Sprint(n))
	}
	return c.print(cmd, http.MethodGet, pathOf(server.IssueEndpoint, id), q, nil)
}
