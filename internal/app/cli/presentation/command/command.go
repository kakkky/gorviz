package command

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/kakkky/gorviz/internal/app/cli/adapter/client"
	"github.com/kakkky/gorviz/internal/app/cli/config"
	"github.com/kakkky/gorviz/internal/protocol/server"
)

const docsHint = "run `gorviz docs` for how to use gorviz"

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
	jsonFlag := &cli.BoolFlag{Name: "json", Usage: "print JSON instead of TOON"}
	return &cli.Command{
		Name:                  "gorviz",
		Usage:                 "diagnose the health of the Go runtime of an app while it runs on your machine",
		Description:           help,
		Flags:                 []cli.Flag{socket, jsonFlag},
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

func (c *Command) do(ctx context.Context, cmd *cli.Command, method, path string, query url.Values, body []byte, json bool) (*client.Response, error) {
	return c.server.Do(ctx, cmd.String("socket"), method, path, query, body, json)
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
	resp, err := c.do(context.Background(), cmd, method, path, query, body, cmd.Bool("json"))
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
	if resp, err := c.do(ctx, cmd, http.MethodGet, server.StatusEndpoint, nil, nil, true); err == nil {
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
		resp, err := c.do(ctx, cmd, http.MethodGet, server.StartResultEndpoint, nil, nil, cmd.Bool("json"))
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

func (c *Command) stop(_ context.Context, cmd *cli.Command) error {
	if err := c.print(cmd, http.MethodPost, server.StopEndpoint, nil, nil); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := c.do(context.Background(), cmd, http.MethodGet, server.StatusEndpoint, nil, nil, true)
		if err != nil {
			return nil
		}
		resp.Body.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return cli.Exit("the gorviz server did not stop; see "+server.LogPath(), 2)
}

func (c *Command) status(_ context.Context, cmd *cli.Command) error {
	return c.print(cmd, http.MethodGet, server.StatusEndpoint, nil, nil)
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
	return c.print(cmd, http.MethodPost, server.DiagnoseEndpoint, nil, scenario)
}

func (c *Command) issues(_ context.Context, cmd *cli.Command) error {
	q := url.Values{}
	for _, f := range []string{"kind", "text", "after"} {
		if v := cmd.String(f); v != "" {
			q.Set(f, v)
		}
	}
	return c.print(cmd, http.MethodGet, server.IssuesEndpoint, q, nil)
}

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

func (c *Command) showDocs(_ context.Context, cmd *cli.Command) error {
	entries, err := fs.ReadDir(c.docs, ".")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if n := strings.TrimSuffix(e.Name(), ".md"); n != "help" {
			names = append(names, n)
		}
	}
	name := cmd.Args().First()
	if name == "" {
		fmt.Fprintln(c.out, "docs:")
		for _, n := range names {
			body, _ := c.doc(n)
			fmt.Fprintf(c.out, "  %s: %s\n", n, summaryOf(body))
		}
		fmt.Fprintln(c.out, "read one with gorviz docs <name>; start with gorviz docs getting-started")
		return nil
	}
	if !slices.Contains(names, name) {
		return cli.Exit(fmt.Sprintf("no document %q; the documents are %s", name, strings.Join(names, ", ")), 2)
	}
	body, err := c.doc(name)
	if err != nil {
		return err
	}
	fmt.Fprint(c.out, body)
	return nil
}

// summaryOf は見出しのすぐ後の 1 行
func summaryOf(body string) string {
	for _, l := range strings.Split(body, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			return l
		}
	}
	return ""
}
