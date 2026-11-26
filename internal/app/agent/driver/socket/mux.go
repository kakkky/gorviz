package socket

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/agent/presentation/middleware"
	socketpresentation "github.com/kakkky/gorviz/internal/app/agent/presentation/socket"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// RequestMux は server からの要求を、method で引いた handler に渡す
type RequestMux struct {
	routes     map[agent.Method]func(ctx context.Context, req agent.Request)
	notFound   func(ctx context.Context, req agent.Request)
	disconnect func(ctx context.Context)
}

func NewRequestMux(h *socketpresentation.Handler, mw *middleware.Middleware) *RequestMux {
	mux := &RequestMux{
		routes:     map[agent.Method]func(ctx context.Context, req agent.Request){},
		notFound:   mw.WrapRequest(h.NotFoundHandler()),
		disconnect: mw.WrapDisconnect(h.DisconnectHandler()),
	}
	for _, route := range h.Routes() {
		mux.routes[route.Method] = mw.WrapRequest(route.Handler)
	}
	return mux
}

func (x *RequestMux) HandleMessage(m agent.Message) {
	if m.Type != agent.TypeRequest {
		return
	}
	req, err := agent.Decode[agent.Request](m)
	if err != nil {
		return
	}
	handle, ok := x.routes[req.Method]
	if !ok {
		handle = x.notFound
	}
	handle(context.Background(), req)
}

func (x *RequestMux) HandleDisconnect() {
	x.disconnect(context.Background())
}
