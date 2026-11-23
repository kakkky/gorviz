// Package command は 1 つの操作を 1 つのサブコマンドとして出す。server の応答は解釈せず、そのまま出す
package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/kakkky/gorviz/internal/docs"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

const docsCommand = "docs"

var ErrUsage = errors.New("usage")

type Command struct {
	toon   *serverapi.Client
	json   *serverapi.Client
	stdout io.Writer
	stderr io.Writer
}

func NewCommand(toon, json *serverapi.Client, stdout, stderr io.Writer) *Command {
	return &Command{toon: toon, json: json, stdout: stdout, stderr: stderr}
}

func (c *Command) Run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		c.usage(c.stdout)
		return nil
	}
	if args[0] == docsCommand {
		return c.docs(args[1:])
	}
	i := slices.IndexFunc(serverapi.AllRoutes, func(r serverapi.Route) bool { return r.Name == args[0] })
	if i < 0 {
		fmt.Fprintf(c.stderr, "unknown command %q\n\n", args[0])
		c.usage(c.stderr)
		return ErrUsage
	}
	return c.call(ctx, serverapi.AllRoutes[i], args[1:])
}

func (c *Command) usage(w io.Writer) {
	fmt.Fprintf(w, "usage: gorviz <command> [--name value ...] [--json]\n\n")
	fmt.Fprintf(w, "まず `gorviz %s` でドキュメントの一覧を見て、`gorviz %s getting-started` を読む。\n\n", docsCommand, docsCommand)
	fmt.Fprintf(w, "commands:\n")
	for _, r := range serverapi.AllRoutes {
		fmt.Fprintf(w, "  %-30s %s\n", r.Name, docs.OperationSummary(r.Name))
	}
	fmt.Fprintf(w, "  %-30s %s\n", docsCommand+" [name]", "ドキュメントの一覧 / 1 つのドキュメント")
}

func (c *Command) docs(args []string) error {
	if len(args) == 0 {
		for _, d := range docs.List() {
			fmt.Fprintf(c.stdout, "%s\t%s\n", d.Name, d.Summary)
		}
		return nil
	}
	body, ok := docs.Get(args[0])
	if !ok {
		fmt.Fprintf(c.stderr, "no document %q. `gorviz %s` lists them\n", args[0], docsCommand)
		return ErrUsage
	}
	fmt.Fprint(c.stdout, body)
	return nil
}

func (c *Command) call(ctx context.Context, route serverapi.Route, args []string) error {
	fs := flag.NewFlagSet(route.Name, flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	fs.Usage = func() {
		body, _ := docs.Operation(route.Name)
		fmt.Fprintf(c.stderr, "usage: gorviz %s [--name value ...] [--json]\n\n%s", route.Name, body)
	}
	values := map[string]*multiValue{}
	for _, p := range route.Params {
		v := &multiValue{}
		values[p.Name] = v
		fs.Var(v, p.Name, string(p.In))
	}
	asJSON := fs.Bool("json", false, "JSON で返す (既定は TOON)")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	params := map[string]string{}
	q := serverapi.Query{}
	var missing []string
	for _, p := range route.Params {
		v := values[p.Name].values
		if len(v) == 0 {
			if p.Required {
				missing = append(missing, "--"+p.Name)
			}
			continue
		}
		if p.In == serverapi.InPath {
			params[p.Name] = v[len(v)-1]
			continue
		}
		q[p.Name] = v
	}
	if len(missing) > 0 {
		fmt.Fprintf(c.stderr, "missing %s. `gorviz %s %s` shows how to use it\n", strings.Join(missing, ", "), docsCommand, "operations/"+route.Name)
		return ErrUsage
	}
	client := c.toon
	if *asJSON {
		client = c.json
	}
	body, err := client.Call(ctx, route, params, q)
	if err != nil {
		c.explain(err)
		return err
	}
	_, err = c.stdout.Write(body)
	return err
}

func (c *Command) explain(err error) {
	var re *serverapi.ResponseError
	if errors.As(err, &re) {
		c.stderr.Write(re.Body)
		fmt.Fprintln(c.stderr)
	}
	if errors.Is(err, serverapi.ErrUnauthorized) {
		body, _ := docs.Get("authentication")
		fmt.Fprintf(c.stderr, "\n%s", body)
		return
	}
	fmt.Fprintf(c.stderr, "`gorviz %s` lists the documents\n", docsCommand)
}

type multiValue struct{ values []string }

func (m *multiValue) String() string { return strings.Join(m.values, ",") }

func (m *multiValue) Set(v string) error {
	m.values = append(m.values, v)
	return nil
}
