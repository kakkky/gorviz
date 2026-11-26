package command

import (
	"context"
	"net/http"
	"net/url"

	"github.com/urfave/cli/v3"

	"github.com/kakkky/gorviz/internal/protocol/server"
)

func (c *Command) goroutine(_ context.Context, cmd *cli.Command) error {
	id := cmd.Args().First()
	if id == "" {
		return cli.Exit("gorviz goroutine needs a goroutine ID, like gorviz goroutine 42; take one from gorviz issue <id>", 2)
	}
	q := url.Values{}
	if f, t := cmd.String("from"), cmd.String("to"); f != "" || t != "" {
		if f == "" || t == "" {
			return cli.Exit("pass both --from and --to, or neither", 2)
		}
		q.Set("from", f)
		q.Set("to", t)
	}
	return c.print(cmd, http.MethodGet, pathOf(server.GoroutineEndpoint, id), q, nil)
}
