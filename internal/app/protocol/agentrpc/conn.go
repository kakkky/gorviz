package agentrpc

import (
	"context"
	"errors"
	"math"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// maxMessageBytes は 1 つのメッセージの上限。gRPC の既定 (4MiB) では、忙しい対象の区切り (全イベント) が収まらない
const maxMessageBytes = 256 << 20

// keepalive の間隔。止まった相手や、途中の LB に黙って切られた接続を早く見つけて、つなぎ直せるようにする
const (
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
)

// NewClientConn は張り終えた conn の上で、相手を呼ぶ gRPC のクライアントを作る。
// conn が切れたら gRPC はつなぎ直さず (呼び出しは Unavailable で失敗する)、つなぎ直すのは接続を張った側に任せる
func NewClientConn(conn net.Conn) (*grpc.ClientConn, error) {
	var used sync.Once
	dial := func(context.Context, string) (net.Conn, error) {
		err := errors.New("the connection is closed: the side that dialed it reconnects")
		used.Do(func() { err = nil })
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
	cc, err := grpc.NewClient("passthrough:///"+conn.RemoteAddr().String(),
		grpc.WithContextDialer(dial),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// 区切りは関数名などの同じ文字列が繰り返し出るので、圧縮すると何分の 1 かになる
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMessageBytes), grpc.UseCompressor(gzip.Name)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: pingInterval, Timeout: pingTimeout, PermitWithoutStream: true}),
		// 使われない間に接続を閉じると、同じ conn をもう張り直せない
		grpc.WithIdleTimeout(0),
	)
	if err != nil {
		return nil, model.ErrInternal.Wrap(err, "create a gRPC client on the connection")
	}
	cc.Connect()
	return cc, nil
}

// Serve は張り終えた conn の上で、相手からの呼び出しを受ける。register でサービスを載せる。conn が切れるか ctx が終わるまで返らない
func Serve(ctx context.Context, conn net.Conn, register func(grpc.ServiceRegistrar)) {
	srv := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMessageBytes),
		grpc.MaxSendMsgSize(math.MaxInt32),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: pingInterval, Timeout: pingTimeout}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: pingInterval / 2, PermitWithoutStream: true}),
	)
	register(srv)
	stop := context.AfterFunc(ctx, srv.Stop)
	defer stop()
	_ = srv.Serve(newConnListener(conn))
	srv.Stop()
}

// connListener は張り終えた 1 本の conn だけを渡す net.Listener。grpc.Server は Accept を呼び続けるので、
// 2 回目からは conn が閉じられるまで待ってからエラーを返し、Serve を終わらせる
type connListener struct {
	conn   chan net.Conn
	closed chan struct{}
	once   sync.Once
	addr   net.Addr
}

func newConnListener(conn net.Conn) *connListener {
	l := &connListener{conn: make(chan net.Conn, 1), closed: make(chan struct{}), addr: conn.LocalAddr()}
	l.conn <- &notifyConn{Conn: conn, closed: l.Close}
	return l
}

func (l *connListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conn:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *connListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *connListener) Addr() net.Addr { return l.addr }

type notifyConn struct {
	net.Conn
	closed func() error
}

func (c *notifyConn) Close() error {
	_ = c.closed()
	return c.Conn.Close()
}
