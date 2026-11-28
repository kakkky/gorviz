package command

import (
	"context"
	"net/http"
	"net/url"

	"github.com/urfave/cli/v3"
)

func (c *Command) goroutineCommand() *cli.Command {
	return &cli.Command{Name: "goroutine", Usage: "show what one goroutine was blocked on", ArgsUsage: "<id>", Action: c.goroutine, Flags: []cli.Flag{
		&cli.StringFlag{Name: "from", Usage: "the start of the range, as time since the session started (like 12.5s)"},
		&cli.StringFlag{Name: "to", Usage: "the end of the range, as time since the session started"},
	}}
}

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
	return c.print(cmd, http.MethodGet, pathOf(goroutineEndpoint, id), q, nil)
}
