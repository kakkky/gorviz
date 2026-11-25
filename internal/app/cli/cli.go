// Package cli は gorviz のサブコマンド。gorviz run 以外は、どれも server の HTTP API のクライアント
package cli

import (
	"context"
	"os"

	"github.com/kakkky/gorviz/internal/app/cli/config"
	"github.com/kakkky/gorviz/internal/app/cli/docs"
	"github.com/kakkky/gorviz/internal/app/cli/presentation/command"
)

func Run(ctx context.Context, cfg *config.Config) error {
	// presentation
	cmd, err := command.NewCommand(cfg, docs.FS)
	if err != nil {
		return err
	}
	return cmd.Run(ctx, os.Args)
}
