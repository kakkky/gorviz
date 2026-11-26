// Package cli は gorviz のサブコマンド。gorviz run は server を起動し、ほかはどれも server の HTTP API を呼ぶ
package cli

import (
	"context"
	"os"

	"github.com/kakkky/gorviz/internal/app/cli/adapter/client"
	"github.com/kakkky/gorviz/internal/app/cli/config"
	"github.com/kakkky/gorviz/internal/app/cli/docs"
	"github.com/kakkky/gorviz/internal/app/cli/presentation/command"
)

func Run(ctx context.Context, cfg *config.Config) error {
	// adapter
	server, err := client.NewServerClient()
	if err != nil {
		return err
	}

	// presentation
	cmd, err := command.NewCommand(cfg, docs.FS, server)
	if err != nil {
		return err
	}
	return cmd.Run(ctx, os.Args)
}
