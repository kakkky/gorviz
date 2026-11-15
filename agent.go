package gorviz

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/kakkky/gorviz/internal/adapter/inprocess"
	"github.com/kakkky/gorviz/internal/config"
	"github.com/kakkky/gorviz/internal/driver/agent"
	"github.com/kakkky/gorviz/internal/logmark"
)

const (
	logQueueSize      = 10_000
	maxFlightRecorder = 64 << 20
)

// running は今動いている埋め込みの agent。1 プロセスは 1 つの観測対象なので、agent も 1 つに限る
var running atomic.Pointer[Agent]

type Options struct {
	// Server は gorviz-server が agent を待ち受けるアドレス (gorviz-server の -agent-addr)
	Server string
	// Name は agent の名前。server はこの名前で束ねる。省くと hostname (レプリカごとに別の名前になる)
	Name string
	// DumpInterval は goroutine dump を取る間隔。dump は全 G を止める (STW) ので間を空ける。省くと 1 分
	DumpInterval time.Duration
	// LeakCheckEvery 回の dump に 1 回、runtime のリーク検査をする (GC が 1 回走る)。省くと 5、負の値で取らない
	LeakCheckEvery int
}

// Agent はアプリに埋め込んだ agent。直近の trace はアプリのメモリ (runtime の FlightRecorder) にあるので、
// プロセスが落ちると直前の録画は残らない
type Agent struct {
	logs   chan []byte
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// Start は agent を裏で動かし始める。ctx が終わるか Stop を呼ぶまで、録画と server へのつなぎを回す
func Start(ctx context.Context, opt Options) (*Agent, error) {
	cfg, err := configOf(opt)
	if err != nil {
		return nil, err
	}
	c, err := inprocess.NewRuntimeClient(cfg.ChunkDuration, uint64(min(cfg.TraceBufferBytes, maxFlightRecorder)))
	if err != nil {
		return nil, err
	}
	a := &Agent{logs: make(chan []byte, logQueueSize), done: make(chan struct{})}
	if !running.CompareAndSwap(nil, a) {
		c.Stop()
		return nil, errors.New("gorviz: an agent is already running in this process")
	}
	logmark.SetForward(a.enqueue)
	ctx, a.cancel = context.WithCancel(ctx)
	r, w := io.Pipe()
	go a.pumpLogs(ctx, w)
	go func() {
		defer close(a.done)
		a.err = agent.New(cfg, c).Run(ctx, agent.LogSource{R: r, Source: "app", Echo: io.Discard})
		logmark.SetForward(nil)
		running.CompareAndSwap(a, nil)
		a.cancel()
		r.Close()
		c.Stop()
	}()
	return a, nil
}

// Stop は agent を止め、止まるまで待つ
func (a *Agent) Stop() error {
	a.cancel()
	<-a.done
	return a.err
}

// enqueue は待たない。agent が読み切れないほどログが多いときは、アプリを止めないよう捨てる
func (a *Agent) enqueue(line []byte) {
	select {
	case a.logs <- line:
	default:
	}
}

func (a *Agent) pumpLogs(ctx context.Context, w *io.PipeWriter) {
	defer w.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-a.logs:
			if _, err := w.Write(line); err != nil {
				return
			}
		}
	}
}

func configOf(opt Options) (*config.Config, error) {
	cfg, err := config.NewConfig("gorviz", nil)
	if err != nil {
		return nil, err
	}
	cfg.AgentAddr, cfg.Name, cfg.TargetURL = opt.Server, opt.Name, "in-process"
	if cfg.Name == "" {
		if cfg.Name, err = os.Hostname(); err != nil {
			return nil, err
		}
	}
	if opt.DumpInterval > 0 {
		cfg.DumpInterval = opt.DumpInterval
	}
	if opt.LeakCheckEvery != 0 {
		cfg.LeakCheckEvery = opt.LeakCheckEvery
	}
	return cfg, nil
}
