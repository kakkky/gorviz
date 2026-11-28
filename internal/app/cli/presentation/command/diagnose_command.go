package command

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"

	"github.com/urfave/cli/v3"
)

func (c *Command) diagnoseCommand() *cli.Command {
	return &cli.Command{Name: "diagnose", Usage: "run the app (under load when a scenario is given) and report issues", ArgsUsage: "[scenario.yaml | -]", Action: c.diagnose}
}

func (c *Command) diagnose(_ context.Context, cmd *cli.Command) error {
	var scenario []byte
	if name := cmd.Args().First(); name != "" {
		var err error
		if name == "-" {
			scenario, err = io.ReadAll(bufio.NewReader(os.Stdin))
		} else {
			scenario, err = os.ReadFile(name)
		}
		if err != nil {
			return cli.Exit("read the scenario: "+err.Error()+"\nsee `gorviz docs scenario`", 2)
		}
	}
	return c.print(cmd, http.MethodPost, diagnoseEndpoint, nil, scenario)
}
