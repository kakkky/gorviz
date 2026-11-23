package launcher

import (
	"context"
	"log/slog"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/agent"
	"github.com/kakkky/gorviz/internal/app/launcher/config"
	"github.com/kakkky/gorviz/internal/app/server"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

func Run(ctx context.Context, cfg *config.Config) error {
	slog.InfoContext(ctx, "gorviz is running", slog.String("gui", cfg.GUIURL),
		slog.String("cli", "GORVIZ_SERVER_URL="+cfg.GUIURL+" "+serverapi.TokenEnv+"="+cfg.Server.BootstrapToken))
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(ctx context.Context) error { return server.Run(ctx, cfg.Server) })
		sc.Go(func(ctx context.Context) error { return agent.Run(ctx, cfg.Agent) })
		return nil
	})
}
