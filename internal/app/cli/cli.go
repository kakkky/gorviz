// Package cli は gorviz (CLI) を組み立てる。server の API のクライアントと録画の書き出し先を usecase に渡し、操作を回す
package cli

import (
	"context"
	"io"

	"github.com/kakkky/gorviz/internal/app/api/serverapi"
	"github.com/kakkky/gorviz/internal/app/cli/adapter/tracefile"
	"github.com/kakkky/gorviz/internal/app/cli/presentation/command"
	"github.com/kakkky/gorviz/internal/app/cli/usecase"
)

func Issues(ctx context.Context, w io.Writer, server, agent string) error {
	return newCommand(w, server, agent).Issues(ctx)
}

func Export(ctx context.Context, w io.Writer, server, agent, out string) error {
	return newCommand(w, server, agent).Export(ctx, out)
}

func newCommand(w io.Writer, server, agent string) *command.Command {
	uc := usecase.NewUseCase(serverapi.NewClient(server, agent), tracefile.NewWriter())
	return command.New(uc, w, server)
}
