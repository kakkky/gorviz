// Package command は gorviz の CLI の非対話の操作。usecase の結果を端末に書く
package command

import (
	"context"
	"fmt"
	"io"

	"github.com/kakkky/gorviz/internal/app/cli/usecase"
)

type Command struct {
	usecase *usecase.UseCase
	w       io.Writer
	// server は G の診断の URL の頭に付ける server の URL
	server string
}

func New(uc *usecase.UseCase, w io.Writer, server string) *Command {
	return &Command{usecase: uc, w: w, server: server}
}

// Issues は検出した Issue を 1 回書く
func (c *Command) Issues(ctx context.Context) error {
	out, err := c.usecase.NewListIssuesUsecase().Exec(ctx, usecase.ListIssuesUsecaseInput{})
	if err != nil {
		return err
	}
	if len(out.Issues) == 0 {
		fmt.Fprintln(c.w, "no issues detected")
	}
	for _, i := range out.Issues {
		fmt.Fprintf(c.w, "%-16s %-9s %s\n  %s\n", i.Kind, i.Certainty, i.Summary, i.Subject)
		if i.Goroutine != nil {
			fmt.Fprintf(c.w, "  diagnose: %s/goroutines/%d?%s\n", c.server, *i.Goroutine, i.GoroutineQuery)
		}
	}
	return nil
}

// Export は保存した録画を out の下に書き出し、書き出した先を 1 件ずつ書く
func (c *Command) Export(ctx context.Context, out string) error {
	res, err := c.usecase.NewExportRecordingsUsecase().Exec(ctx, usecase.ExportRecordingsUsecaseInput{Out: out})
	for _, r := range res.Exported {
		fmt.Fprintf(c.w, "%s: %d chunks → %s (go tool trace %s/chunk-0.trace)\n", r.ID, r.Chunks, r.Location, r.Location)
	}
	if err != nil {
		return err
	}
	if len(res.Exported) == 0 {
		fmt.Fprintln(c.w, "no saved recordings")
	}
	return nil
}
