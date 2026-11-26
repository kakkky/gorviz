package middleware

import (
	"context"
	"fmt"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/socket"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

// Middleware は、1 つの要求の失敗や panic で接続を止めない。エラーは server への応答に載せる
type Middleware struct {
	sender domain.ServerSender
}

func NewMiddleware(sender domain.ServerSender) *Middleware {
	return &Middleware{sender: sender}
}

// WrapRequest は要求を handler に渡し、ID を付けた応答を返す
func (m *Middleware) WrapRequest(next socket.RequestHandler) func(ctx context.Context, req agent.Request) {
	return func(ctx context.Context, req agent.Request) {
		res := domain.Response{ID: req.ID}
		if err := recovered(func() error { return next.HandleRequest(ctx, req, &res) }); err != nil {
			res.Error = err.Error()
		}
		_ = m.sender.SendResponse(res)
	}
}

func (m *Middleware) WrapDisconnect(next socket.DisconnectHandler) func(ctx context.Context) {
	return func(ctx context.Context) {
		_ = recovered(func() error { return next.HandleDisconnect(ctx) })
	}
}

func recovered(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = domain.ErrInternal.With(fmt.Sprint("panic: ", r))
		}
	}()
	return f()
}
