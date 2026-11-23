// Package interceptor は agent からの gRPC の手前で、トークンの確認・アクセスログ・エラーの返し方・panic の復旧をまとめて扱う
package interceptor

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
)

// TokenChecker は agent のトークンが有効か
type TokenChecker interface {
	ValidAgentToken(token string) bool
}

type Interceptor struct {
	tokens TokenChecker
}

func NewInterceptor(tokens TokenChecker) *Interceptor {
	return &Interceptor{tokens: tokens}
}

func (ic *Interceptor) Unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (res any, err error) {
	start := time.Now()
	hs, ok := agentrpc.HandshakeFromContext(ctx)
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "panic in a grpc handler", slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
			res, err = nil, status.Error(codes.Internal, "internal error")
		}
		code := status.Code(err)
		attrs := []any{slog.String("method", info.FullMethod), slog.String("agent", string(hs.AgentID)), slog.String("code", code.String()), slog.Duration("took", time.Since(start))}
		if code == codes.Internal {
			slog.ErrorContext(ctx, "grpc request", append(attrs, slog.Any("err", err))...)
		} else {
			slog.InfoContext(ctx, "grpc request", attrs...)
		}
	}()
	if !ok || !ic.tokens.ValidAgentToken(hs.Token) {
		return nil, status.Error(codes.Unauthenticated, "the gorviz server does not know the agent token")
	}
	res, err = handler(ctx, req)
	if err != nil {
		return nil, toStatus(err)
	}
	return res, nil
}

func toStatus(err error) error {
	var de *domain.Error
	if !errors.As(err, &de) {
		return status.Error(codes.Internal, "internal error")
	}
	switch {
	case errors.Is(err, domain.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, de.Msg)
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, de.Msg)
	}
	return status.Error(codes.Internal, "internal error")
}
