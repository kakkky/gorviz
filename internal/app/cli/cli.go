package cli

import (
	"context"
	"os"

	"github.com/kakkky/gorviz/internal/app/cli/config"
	"github.com/kakkky/gorviz/internal/app/cli/presentation/command"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

func Run(ctx context.Context, cfg *config.Config) error {
	hc, err := serverapi.NewHTTPClient(cfg.CAFile, cfg.CAFingerprint)
	if err != nil {
		return err
	}
	toon := serverapi.NewClient(cfg.ServerURL, cfg.Token, serverapi.MediaTypeTOON, hc)
	json := serverapi.NewClient(cfg.ServerURL, cfg.Token, serverapi.MediaTypeJSON, hc)
	return command.NewCommand(toon, json, os.Stdout, os.Stderr).Run(ctx, os.Args[1:])
}
