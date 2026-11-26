package middleware

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kakkky/gorviz/internal/app/server/presentation/socket"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// WrapMessage は、1 つのメッセージの失敗や panic を記録して捨てる。ほかのメッセージを読めなくしない
func (m *Middleware) WrapMessage(t agent.MessageType, next socket.MessageHandler) func(ctx context.Context, msg agent.Message) {
	return func(ctx context.Context, msg agent.Message) {
		logged(ctx, t, func() error { return next.HandleMessage(ctx, msg) })
	}
}

func (m *Middleware) WrapDisconnect(next socket.DisconnectHandler) func(ctx context.Context, clean bool) {
	return func(ctx context.Context, clean bool) {
		logged(ctx, 0, func() error { return next.HandleDisconnect(ctx, clean) })
	}
}

func logged(ctx context.Context, t agent.MessageType, f func() error) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "panic while handling an agent message", slog.Int("type", int(t)), slog.String("panic", fmt.Sprint(r)))
		}
	}()
	if err := f(); err != nil {
		slog.ErrorContext(ctx, "handle an agent message", slog.Int("type", int(t)), slog.Any("err", err))
	}
}
