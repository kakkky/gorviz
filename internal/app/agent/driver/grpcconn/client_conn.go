package grpcconn

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

// ClientConn は server への接続 1 本。切れたら gRPC が張り直す。張るのは最初の送信のときで、server がまだいなくても起動できる
type ClientConn struct {
	*grpc.ClientConn
}

func NewClientConn(cfg *config.Config) (*ClientConn, func(), error) {
	base := insecure.NewCredentials()
	if !cfg.Insecure {
		tlsCfg, err := serverapi.TLSConfig(cfg.CAFile, cfg.CAFingerprint)
		if err != nil {
			return nil, nil, err
		}
		base = credentials.NewTLS(tlsCfg)
	}
	opts := append(agentrpc.DialOptions(), grpc.WithTransportCredentials(agentrpc.NewClientCredentials(base, cfg.AgentID, cfg.Token)))
	conn, err := grpc.NewClient(cfg.ServerAddr, opts...)
	if err != nil {
		return nil, nil, err
	}
	return &ClientConn{ClientConn: conn}, func() { _ = conn.Close() }, nil
}
