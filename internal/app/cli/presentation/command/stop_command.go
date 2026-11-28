package command

import (
	"context"
	"net/http"
	"time"

	"github.com/urfave/cli/v3"
)

func (c *Command) stopCommand() *cli.Command {
	return &cli.Command{Name: "stop", Usage: "watch the shutdown of the app, then stop the session", Action: c.stop}
}

func (c *Command) stop(_ context.Context, cmd *cli.Command) error {
	if err := c.print(cmd, http.MethodPost, stopEndpoint, nil, nil); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := c.do(context.Background(), cmd, http.MethodGet, statusEndpoint, nil, nil, mediaJSON)
		if err != nil {
			return nil
		}
		resp.Body.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return cli.Exit("the gorviz server did not stop; see "+c.cfg.ServerLog, 2)
}
