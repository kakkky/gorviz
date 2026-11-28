package command

import (
	"context"
	"net/http"
	"net/url"

	"github.com/urfave/cli/v3"
)

func (c *Command) issuesCommand() *cli.Command {
	return &cli.Command{Name: "issues", Usage: "list the issues of the session", Action: c.issues, Flags: []cli.Flag{
		&cli.StringFlag{Name: "kind", Usage: "only issues of this kind"},
		&cli.StringFlag{Name: "text", Usage: "only issues whose target contains this text"},
		&cli.StringFlag{Name: "after", Usage: "continue the list after this issue ID"},
	}}
}

func (c *Command) issues(_ context.Context, cmd *cli.Command) error {
	q := url.Values{}
	for _, f := range []string{"kind", "text", "after"} {
		if v := cmd.String(f); v != "" {
			q.Set(f, v)
		}
	}
	return c.print(cmd, http.MethodGet, issuesEndpoint, q, nil)
}
