package gorviz

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent"
)

// running は今動いている埋め込みの agent。1 プロセスは 1 つの観測対象なので、agent も 1 つに限る
var running atomic.Pointer[Agent]

type Options struct {
	// Server は gorviz-server が agent を待ち受けるアドレス (gorviz-server の -agent-addr)
	Server string
	// Name は agent の名前。server はこの名前で束ねる。省くと hostname (レプリカごとに別の名前になる)。Cloud Run では instance ID を足す
	Name string
	// DumpInterval は goroutine dump を取る間隔。dump は全 G を止める (STW) ので間を空ける。省くと 1 分
	DumpInterval time.Duration
	// LeakCheckEvery 回の dump に 1 回、runtime のリーク検査をする (GC が 1 回走る)。省くと 5、負の値で取らない
	LeakCheckEvery int
}

// Agent はアプリに埋め込んだ agent。直近の trace はアプリのメモリ (runtime の FlightRecorder) にあるので、
// プロセスが落ちると直前の録画は残らない
type Agent struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// Start は agent を裏で動かし始める。ctx が終わるか Stop を呼ぶまで、録画と server へのつなぎを回す
func Start(ctx context.Context, opt Options) (*Agent, error) {
	e, err := agent.NewEmbedded(ctx, agent.EmbedOptions{Server: opt.Server, Name: opt.Name, DumpInterval: opt.DumpInterval, LeakCheckEvery: opt.LeakCheckEvery})
	if err != nil {
		return nil, err
	}
	a := &Agent{done: make(chan struct{})}
	if !running.CompareAndSwap(nil, a) {
		e.Close()
		return nil, errors.New("gorviz: an agent is already running in this process")
	}
	ctx, a.cancel = context.WithCancel(ctx)
	go func() {
		defer close(a.done)
		a.err = e.Run(ctx)
		running.CompareAndSwap(a, nil)
		a.cancel()
	}()
	return a, nil
}

// Stop は agent を止め、止まるまで待つ
func (a *Agent) Stop() error {
	a.cancel()
	<-a.done
	return a.err
}
