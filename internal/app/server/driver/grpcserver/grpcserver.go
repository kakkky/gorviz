// Package grpcserver は agent からの接続を待ち受ける
package grpcserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/kakkky/scope"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
)

const stopTimeout = 10 * time.Second

type GRPCServer struct {
	lis net.Listener
	srv *grpc.Server
}

// NewGRPCServer は待ち受けを開く。tlsConf が nil なら平文で待ち受ける
func NewGRPCServer(cfg *config.Config, tlsConf *tls.Config, unary grpc.UnaryServerInterceptor, svc agentrpc.ObservationServiceServer) (*GRPCServer, func(), error) {
	base := insecure.NewCredentials()
	if tlsConf != nil {
		base = credentials.NewTLS(tlsConf)
	}
	lis, err := net.Listen("tcp", cfg.AgentAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen for agents on %s: %w", cfg.AgentAddr, err)
	}
	opts := append(agentrpc.ServerOptions(), grpc.Creds(agentrpc.NewServerCredentials(base)), grpc.ChainUnaryInterceptor(unary))
	srv := grpc.NewServer(opts...)
	agentrpc.RegisterObservationServiceServer(srv, svc)
	return &GRPCServer{lis: lis, srv: srv}, func() { lis.Close() }, nil
}

func (s *GRPCServer) Run(ctx context.Context) error {
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(ctx context.Context) error {
			if err := s.srv.Serve(s.lis); err != nil && ctx.Err() == nil {
				return fmt.Errorf("serve agents: %w", err)
			}
			return nil
		})
		sc.Go(func(ctx context.Context) error {
			<-ctx.Done()
			timer := time.AfterFunc(stopTimeout, s.srv.Stop)
			defer timer.Stop()
			s.srv.GracefulStop()
			return nil
		})
		return nil
	})
}
