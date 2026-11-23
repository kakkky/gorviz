// Package agentrpc は agent と server の契約。gRPC の形と、domain/observation との詰め替えと、接続の決まり (ハンドシェイク・keepalive・圧縮) を持つ
package agentrpc

import (
	"math"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
)

// maxMessageBytes は gRPC の既定 (4MiB) では、忙しいアプリのチャンクが収まらないため
const maxMessageBytes = 256 << 20

const (
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
)

func DialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMessageBytes), grpc.MaxCallSendMsgSize(maxMessageBytes), grpc.UseCompressor(gzip.Name)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: pingInterval, Timeout: pingTimeout, PermitWithoutStream: true}),
	}
}

func ServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.MaxRecvMsgSize(maxMessageBytes),
		grpc.MaxSendMsgSize(math.MaxInt32),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: pingInterval, Timeout: pingTimeout}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: pingInterval / 2, PermitWithoutStream: true}),
	}
}
