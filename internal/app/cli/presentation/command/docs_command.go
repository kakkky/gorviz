package command

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"
)

func (c *Command) docsCommand() *cli.Command {
	return &cli.Command{Name: "docs", Usage: "list the documents, or print one", ArgsUsage: "[name]", Action: c.showDocs}
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
