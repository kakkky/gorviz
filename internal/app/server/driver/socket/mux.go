package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	socketpresentation "github.com/kakkky/gorviz/internal/app/server/presentation/socket"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// MessageMux は agent から届いたメッセージを、型で引いた handler に渡す。route のない型 (bye など) は捨てる
type MessageMux struct {
	routes     map[agent.MessageType]func(ctx context.Context, m agent.Message)
	disconnect func(ctx context.Context, clean bool)
}

func NewMessageMux(h *socketpresentation.Handler, mw *middleware.Middleware) *MessageMux {
	mux := &MessageMux{
		routes:     map[agent.MessageType]func(ctx context.Context, m agent.Message){},
		disconnect: mw.WrapDisconnect(h.DisconnectHandler()),
	}
	for _, route := range h.Routes() {
		mux.routes[route.Type] = mw.WrapMessage(route.Type, route.Handler)
	}
	return mux
}

func (x *MessageMux) HandleMessage(ctx context.Context, m agent.Message) {
	if handle, ok := x.routes[m.Type]; ok {
		handle(ctx, m)
	}
}

func (x *MessageMux) HandleDisconnect(ctx context.Context, clean bool) {
	x.disconnect(ctx, clean)
}
