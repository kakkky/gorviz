package command

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/kakkky/gorviz/internal/protocol/server"
)

func (c *Command) run(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return cli.Exit("gorviz run needs a main package, like gorviz run ./cmd/api -- --port 8080\n"+docsHint, 2)
	}
	pkg, appArgs := args[0], args[1:]
	if len(appArgs) > 0 && appArgs[0] == "--" {
		appArgs = appArgs[1:]
	}
	socket := cmd.String("socket")
	if resp, err := c.do(ctx, cmd, http.MethodGet, server.StatusEndpoint, nil, nil, server.MediaJSON); err == nil {
		resp.Body.Close()
		return cli.Exit(fmt.Sprintf("a gorviz session already runs on %s; stop it with gorviz stop first", socket), 2)
	}
	dir, err := os.Getwd()
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	if err := c.server.StartServer(socket, dir, pkg, appArgs); err != nil {
		return cli.Exit("start the gorviz server: "+err.Error(), 2)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := c.do(ctx, cmd, http.MethodGet, server.StartResultEndpoint, nil, nil, acceptOf(cmd))
		if err == nil {
			defer resp.Body.Close()
			if _, err := io.Copy(c.out, resp.Body); err != nil {
				return cli.Exit("read the response: "+err.Error(), 2)
			}
			if code := resp.ExitCode(); code != 0 {
				return cli.Exit("", code)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return cli.Exit(fmt.Sprintf("the gorviz server did not start (%v); see %s", err, server.LogPath()), 2)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
