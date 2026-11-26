package command

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/kakkky/gorviz/internal/app/cli/adapter/client"
	"github.com/kakkky/gorviz/internal/app/cli/config"
	"github.com/kakkky/gorviz/internal/protocol/server"
)

const docsHint = "run `gorviz docs` for how to use gorviz"

const formatTOON = "toon"

var formats = map[string]string{formatTOON: server.MediaTOON, "json": server.MediaJSON}

func acceptOf(cmd *cli.Command) string {
	return formats[cmd.String("format")]
}

// Command は gorviz のサブコマンドの定義と、各サブコマンドの Action。server の応答は解釈せず、そのまま出す
type Command struct {
	cfg    *config.Config
	docs   fs.FS
	server *client.ServerClient
	out    io.Writer
}

func NewCommand(cfg *config.Config, docs fs.FS, server *client.ServerClient) (*cli.Command, error) {
	c := &Command{cfg: cfg, docs: docs, server: server, out: os.Stdout}
	help, err := c.doc("help")
	if err != nil {
		return nil, err
	}
	socket := &cli.StringFlag{Name: "socket", Usage: "the unix socket of the gorviz server", Value: cfg.Socket}
	format := &cli.StringFlag{Name: "format", Usage: "the output format, toon or json", Value: formatTOON, Validator: func(v string) error {
		if _, ok := formats[v]; !ok {
			return fmt.Errorf("--format is toon or json, not %q", v)
		}
		return nil
	}}
	return &cli.Command{
		Name:                  "gorviz",
		Usage:                 "diagnose the health of the Go runtime of an app while it runs on your machine",
		Description:           help,
		Flags:                 []cli.Flag{socket, format},
		EnableShellCompletion: false,
		OnUsageError: func(_ context.Context, _ *cli.Command, err error, _ bool) error {
			return cli.Exit(err.Error()+"\n"+docsHint, 2)
		},
		CommandNotFound: func(_ context.Context, _ *cli.Command, name string) {
			fmt.Fprintf(os.Stderr, "no command %q; %s\n", name, docsHint)
			os.Exit(2)
		},
		Commands: []*cli.Command{
			{Name: "run", Usage: "build the app with the agent, start it and return when it runs", ArgsUsage: "<package> [-- app arguments]", Action: c.run},
			{Name: "stop", Usage: "watch the shutdown of the app, then stop the session", Action: c.stop},
			{Name: "status", Usage: "show whether the app runs, the GUI URL and the diagnosed ranges", Action: c.status},
			{Name: "diagnose", Usage: "run the app (under load when a scenario is given) and report issues", ArgsUsage: "[scenario.yaml | -]", Action: c.diagnose},
			{Name: "issues", Usage: "list the issues of the session", Action: c.issues, Flags: []cli.Flag{
				&cli.StringFlag{Name: "kind", Usage: "only issues of this kind"},
				&cli.StringFlag{Name: "text", Usage: "only issues whose target contains this text"},
				&cli.StringFlag{Name: "after", Usage: "continue the list after this issue ID"},
			}},
			{Name: "issue", Usage: "open an issue with the goroutines that were affected the most", ArgsUsage: "<id>", Action: c.issue, Flags: []cli.Flag{
				&cli.IntFlag{Name: "occurrence", Usage: "the occurrence to open, from 1 (the latest by default)"},
			}},
			{Name: "goroutine", Usage: "show what one goroutine was blocked on", ArgsUsage: "<id>", Action: c.goroutine, Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "the start of the range, as time since the session started (like 12.5s)"},
				&cli.StringFlag{Name: "to", Usage: "the end of the range, as time since the session started"},
			}},
			{Name: "docs", Usage: "list the documents, or print one", ArgsUsage: "[name]", Action: c.showDocs},
		},
	}, nil
}

func (c *Command) doc(name string) (string, error) {
	b, err := fs.ReadFile(c.docs, name+".md")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *Command) do(ctx context.Context, cmd *cli.Command, method, path string, query url.Values, body []byte, accept string) (*client.Response, error) {
	return c.server.Do(ctx, cmd.String("socket"), method, path, query, body, accept)
}

// pathOf はルートの {id} を埋める
func pathOf(route, id string) string {
	return strings.Replace(route, "{id}", url.PathEscape(id), 1)
}

func notRunning(cmd *cli.Command, err error) error {
	return cli.Exit(fmt.Sprintf("no gorviz session runs on %s (%v); start one with gorviz run <package>\n%s", cmd.String("socket"), err, docsHint), 2)
}

// print は server の応答をそのまま出し、server が載せた終了コードで終わる
func (c *Command) print(cmd *cli.Command, method, path string, query url.Values, body []byte) error {
	resp, err := c.do(context.Background(), cmd, method, path, query, body, acceptOf(cmd))
	if err != nil {
		return notRunning(cmd, err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(c.out, resp.Body); err != nil {
		return cli.Exit("read the response: "+err.Error(), 2)
	}
	if code := resp.ExitCode(); code != 0 {
		return cli.Exit("", code)
	}
	return nil
}
