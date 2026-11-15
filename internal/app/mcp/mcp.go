// Package mcp は gorviz-mcp を組み立てる。server の API のクライアントを usecase に渡し、MCP サーバを回す
package mcp

import (
	"cmp"
	"context"
	"io"

	"github.com/kakkky/gorviz/internal/app/api/serverapi"
	"github.com/kakkky/gorviz/internal/app/mcp/domain"
	"github.com/kakkky/gorviz/internal/app/mcp/presentation/handler"
	"github.com/kakkky/gorviz/internal/app/mcp/usecase"
)

// Serve は r から読み w に返す MCP サーバを回す。ツールで agent を指定しなければ agent を使う。r を読み切るまで返らない
func Serve(ctx context.Context, r io.Reader, w io.Writer, server, agent string) error {
	serverFor := func(name string) domain.Server { return serverapi.NewClient(server, cmp.Or(name, agent)) }
	return handler.New(usecase.NewUseCase(serverFor), server).Serve(ctx, r, w)
}
