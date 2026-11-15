package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/config"
)

type HTTPServer struct {
	srv *http.Server
}

func NewHTTPServer(cfg *config.Config, mux http.Handler) *HTTPServer {
	return &HTTPServer{
		srv: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
			ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
		},
	}
}

func (s *HTTPServer) Run(ctx context.Context) error {
	// SSE のハンドラは r.Context() の終了で抜けるので、停止時は BaseContext ごと切って接続を閉じさせる
	baseCtx, cancelBase := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelBase()
	s.srv.BaseContext = func(net.Listener) context.Context { return baseCtx }

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(ctx context.Context) error {
			slog.InfoContext(ctx, "gorviz GUI is running", slog.String("url", "http://"+s.srv.Addr))
			if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		})
		sc.Go(func(ctx context.Context) error {
			<-ctx.Done()
			cancelBase()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.srv.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("http server shutdown: %w", err)
			}
			return nil
		})
		return nil
	})
}
