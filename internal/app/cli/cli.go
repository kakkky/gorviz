// Package cli は gorviz (CLI) を組み立てる。server の API のクライアントを usecase に渡し、操作を回す
package cli

import (
	"context"
	"io"

	"github.com/kakkky/gorviz/internal/app/cli/presentation/command"
	"github.com/kakkky/gorviz/internal/app/cli/usecase"
	"github.com/kakkky/gorviz/internal/app/protocol/serverapi"
)

func Issues(ctx context.Context, w io.Writer, server, agent string) error {
	return newCommand(w, server, agent).Issues(ctx)
}

func newCommand(w io.Writer, server, agent string) *command.Command {
	uc := usecase.NewUseCase(serverapi.NewClient(server, agent))
	return command.New(uc, w, server)
}
