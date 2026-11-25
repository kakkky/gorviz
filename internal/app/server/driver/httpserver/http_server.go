package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/kakkky/scope"
)

const shutdownTimeout = 30 * time.Second

// HTTPServer は GUI と HTTP API を待ち受ける。止めるときは、やり取りの途中のリクエスト (gorviz stop など) が終わるのを待つ
type HTTPServer struct {
	server   *http.Server
	listener net.Listener
}

// NewHTTPServer は待ち受けを先に開き、同じアドレスでもう server が動いていれば起動させない
func NewHTTPServer(addr string, h http.Handler) (*HTTPServer, func(), error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	s := &HTTPServer{server: &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}, listener: l}
	return s, func() { _ = l.Close() }, nil
}

func (s *HTTPServer) Run(ctx context.Context) error {
	return scope.Run(context.WithoutCancel(ctx), func(sc *scope.Scope) error {
		sc.Go(func(sctx context.Context) error {
			select {
			case <-ctx.Done():
			case <-sctx.Done():
			}
			tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
			defer cancel()
			_ = s.server.Shutdown(tctx)
			return nil
		})
		sc.Go(func(context.Context) error {
			if err := s.server.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		})
		return nil
	})
}
