// Package httpserver は GUI と HTTP API を待ち受ける
package httpserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/server/config"
)

const shutdownTimeout = 10 * time.Second

type HTTPServer struct {
	lis net.Listener
	srv *http.Server
}

// NewHTTPServer は待ち受けを開く。tlsConf が nil なら平文で待ち受ける
func NewHTTPServer(cfg *config.Config, tlsConf *tls.Config, handler http.Handler) (*HTTPServer, func(), error) {
	lis, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen for http on %s: %w", cfg.HTTPAddr, err)
	}
	if tlsConf != nil {
		lis = tls.NewListener(lis, tlsConf)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	return &HTTPServer{lis: lis, srv: srv}, func() { lis.Close() }, nil
}

func (s *HTTPServer) Run(ctx context.Context) error {
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(ctx context.Context) error {
			if err := s.srv.Serve(s.lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("serve http: %w", err)
			}
			return nil
		})
		sc.Go(func(ctx context.Context) error {
			<-ctx.Done()
			sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
			defer cancel()
			s.srv.Shutdown(sctx)
			return nil
		})
		return nil
	})
}
