package mcp

import (
	"context"
	"os"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/mcp/config"
	"github.com/kakkky/gorviz/internal/app/mcp/driver/mcpserver"
	"github.com/kakkky/gorviz/internal/app/mcp/presentation/handler"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

func Run(ctx context.Context, cfg *config.Config) error {
	hc, err := serverapi.NewHTTPClient(cfg.CAFile, cfg.CAFingerprint)
	if err != nil {
		return err
	}
	client := serverapi.NewClient(cfg.ServerURL, cfg.Token, serverapi.MediaTypeTOON, hc)

	toolHandler := handler.NewToolHandler(client)

	srv := mcpserver.NewMCPServer(os.Stdin, os.Stdout, toolHandler)

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(srv.Run)
		return nil
	})
}
