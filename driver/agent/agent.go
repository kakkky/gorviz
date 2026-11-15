// Package agent は対象 1 つを観測する agent。取る・持つ・求められたら返すだけで、判定はしない。
// cmd/agent (daemon 型・sidecar 型) と、アプリに埋め込むライブラリ型の両方がこれを使う
package agent

import (
	"context"
	"io"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/adapter/agentrpc"
	"github.com/kakkky/gorviz/adapter/kvs"
	"github.com/kakkky/gorviz/adapter/logparse"
	"github.com/kakkky/gorviz/adapter/recording"
	"github.com/kakkky/gorviz/adapter/schedtrace"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/driver/loginput"
	"github.com/kakkky/gorviz/driver/recorder"
	"github.com/kakkky/gorviz/usecase"
)

type Agent struct {
	cfg      *config.Config
	hello    agentrpc.Hello
	recorder *recorder.Recorder
	logInput *loginput.LogInput
	service  *agentrpc.Service
}

// LogSource は取り込むログの流れ。Echo にはそのまま流す (対象のログを端末にも出すため)
type LogSource struct {
	R      io.Reader
	Source string
	Echo   io.Writer
}

// New は runtimeClient (pprof を叩く実装か、アプリの中で取る実装) から trace・dump を取る agent を組み立てる
func New(cfg *config.Config, runtimeClient domain.RuntimeClient, sched *schedtrace.Store) *Agent {
	traces := recording.NewTraceStore(cfg)
	uc := usecase.NewAgentUseCase(cfg, runtimeClient, kvs.NewInMemoryKVS(), sched, traces, recording.NewLogStore(cfg), logparse.NewParser(cfg))
	return &Agent{
		cfg:      cfg,
		hello:    agentrpc.Hello{Name: cfg.Name, Target: cfg.TargetURL},
		recorder: recorder.NewRecorder(cfg, uc),
		logInput: loginput.NewLogInput(uc),
		service:  agentrpc.NewService(traces, uc.NewRecordDumpUsecase(), uc, sched),
	}
}

// Run は録画と、ログの取り込みと、server へのつなぎを回す。ctx が終わるまで返らない
func (a *Agent) Run(ctx context.Context, logs ...LogSource) error {
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(a.recorder.Run)
		sc.Go(func(ctx context.Context) error {
			return agentrpc.Dial(ctx, a.cfg.AgentAddr, a.hello, a.service)
		})
		for _, l := range logs {
			sc.Go(func(ctx context.Context) error {
				return a.logInput.Consume(ctx, l.R, l.Source, l.Echo)
			})
		}
		return nil
	})
}
