package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/kakkky/scope"
)

const shutdownTimeout = 30 * time.Second

// HTTPServer は HTTP を待ち受ける。止めるときは、やり取りの途中のリクエスト (gorviz stop など) が終わるのを待つ
type HTTPServer struct {
	server   *http.Server
	listener net.Listener
}

func newHTTPServer(l net.Listener, h http.Handler) *HTTPServer {
	return &HTTPServer{server: &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}, listener: l}
}

// NewTCPHTTPServer は addr を TCP で待ち受ける。ブラウザから開く GUI のため
func NewTCPHTTPServer(addr string, h http.Handler) (*HTTPServer, func(), error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	return newHTTPServer(l, h), func() { _ = l.Close() }, nil
}

// NewUnixHTTPServer は path の unix socket で待ち受ける。同じ path でもう server が動いていれば起動させず、前の server が残した socket のファイルは消して使う
func NewUnixHTTPServer(path string, h http.Handler) (*HTTPServer, func(), error) {
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		_ = c.Close()
		return nil, nil, fmt.Errorf("a gorviz server already listens on %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, err
	}
	return newHTTPServer(l, h), func() {
		_ = l.Close()
		_ = os.Remove(path)
	}, nil
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
