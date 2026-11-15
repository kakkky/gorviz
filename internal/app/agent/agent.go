// Package agent は agent を組み立てる。agent は対象 1 つを観測し、取る・持つ・軽いものを送る・trace を求められたら返すだけで、判定はしない。
// cmd/agent (daemon 型・sidecar 型) は RunTargets を、アプリに埋め込むライブラリ型は New を使う
package agent

import (
	"context"
	"io"
	"strings"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/client"
	"github.com/kakkky/gorviz/internal/app/agent/adapter/instance"
	"github.com/kakkky/gorviz/internal/app/agent/adapter/kvs"
	"github.com/kakkky/gorviz/internal/app/agent/adapter/logparse"
	"github.com/kakkky/gorviz/internal/app/agent/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/serverconn"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/loginput"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/pull"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/push"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/app/agent/worker"
	"github.com/kakkky/gorviz/internal/app/api/agentrpc"
	"github.com/kakkky/gorviz/internal/runtimeparse"
)

type Agent struct {
	cfg      *config.Config
	hello    agentrpc.Hello
	record   *worker.RecordWorker
	logInput *loginput.LogInput
	pull     *pull.Service
	push     *push.Stream
}

// LogSource は取り込むログの流れ。Echo にはそのまま流す (対象のログを端末にも出すため)
type LogSource struct {
	R      io.Reader
	Source string
	Echo   io.Writer
}

// New は runtimeClient (pprof を叩く実装か、アプリの中で取る実装) から trace・dump を取る agent を組み立てる
func New(cfg *config.Config, runtimeClient domain.RuntimeClient) *Agent {
	uc := usecase.NewUseCase(usecase.Settings{ChunkDuration: cfg.ChunkDuration, LeakCheckEvery: cfg.LeakCheckEvery}, runtimeClient, kvs.NewInMemoryKVS(), recording.NewTraceStore(cfg.TraceBufferBytes), recording.NewLogStore(cfg.MaxLogLines), logparse.NewParser(cfg))
	return &Agent{
		cfg:      cfg,
		hello:    agentrpc.Hello{Name: cfg.Name, Target: cfg.TargetURL},
		record:   worker.NewRecordWorker(cfg, uc),
		logInput: loginput.NewLogInput(uc),
		pull:     pull.NewService(uc),
		push:     push.NewStream(cfg.Refresh, uc),
	}
}

// Run は録画と、ログの取り込みと、server への 2 本の接続 (軽いものの push・trace の pull) を回す。ctx が終わるまで返らない
func (a *Agent) Run(ctx context.Context, logs ...LogSource) error {
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(a.record.Run)
		sc.Go(func(ctx context.Context) error {
			return serverconn.Push(ctx, a.cfg.AgentAddr, a.hello, a.push.Run)
		})
		sc.Go(func(ctx context.Context) error {
			return serverconn.ServePull(ctx, a.cfg.AgentAddr, a.hello, a.pull)
		})
		for _, l := range logs {
			sc.Go(func(ctx context.Context) error {
				return a.logInput.Consume(ctx, l.R, l.Source, l.Echo)
			})
		}
		return nil
	})
}

// RunTargets は gorviz-agent (daemon 型・sidecar 型) の本体。cfg.TargetURL のカンマ区切りの対象ごとに、別の agent として server につなぐ。
// cfg.LogsFrom が "-" で対象が 1 つなら、stdin をその対象のログとして取り込む。ctx が終わるまで返らない
func RunTargets(ctx context.Context, cfg *config.Config, stdin io.Reader) error {
	name, err := instance.Name(ctx, cfg.Name)
	if err != nil {
		return err
	}
	targets := strings.Split(cfg.TargetURL, ",")
	return scope.Run(ctx, func(sc *scope.Scope) error {
		for _, target := range targets {
			c := *cfg
			c.TargetURL = strings.TrimSpace(target)
			c.Name = name
			if len(targets) > 1 {
				c.Name = instance.ForTarget(name, c.TargetURL)
			}
			var logs []LogSource
			if cfg.LogsFrom == "-" && len(targets) == 1 {
				logs = append(logs, LogSource{R: stdin, Source: "stdin", Echo: io.Discard})
			}
			a := New(&c, client.NewRuntimeClient(&c, runtimeparse.NewParser()))
			sc.Go(func(ctx context.Context) error { return a.Run(ctx, logs...) })
		}
		return nil
	})
}
