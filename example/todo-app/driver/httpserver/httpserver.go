package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"
)

type HTTPServer struct {
	name string
	srv  *http.Server
}

func NewHTTPServer(name, addr string, h http.Handler) *HTTPServer {
	return &HTTPServer{name: name, srv: &http.Server{Addr: addr, Handler: h}}
}

// NewPprofServer は net/http/pprof をアプリとは別の口で公開する。gorviz-agent はここから execution trace とプロファイルを取る
func NewPprofServer(addr string) *HTTPServer {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return NewHTTPServer("pprof", addr, mux)
}

func (s *HTTPServer) Run(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdown)
	})
	defer stop()
	slog.InfoContext(ctx, "listening", slog.String("server", s.name), slog.String("url", "http://"+s.srv.Addr))
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
