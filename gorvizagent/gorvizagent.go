// Package gorvizagent はアプリに埋め込む agent (ライブラリ型)。agent を隣に置けない環境 (sidecar を常時動かせないなど) で使う。
//
// 直近の trace はアプリのメモリ (runtime の FlightRecorder) にあるので、プロセスが落ちると直前の録画は残らない。
// trace の負荷は agent を隣に置くときと変わらない (軽くなるのは、録画を別のプロセスが読み取って運ぶぶんだけ)。
//
//	a, err := gorvizagent.New(gorvizagent.Options{Server: "gorviz-server:7071", Name: "api"})
//	logger := slog.New(slogtrace.Wrap(slog.NewJSONHandler(io.MultiWriter(os.Stderr, a.LogWriter()), nil)))
//	go a.Run(ctx)
package gorvizagent

import (
	"context"
	"io"

	"github.com/kakkky/gorviz/adapter/inprocess"
	"github.com/kakkky/gorviz/adapter/schedtrace"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/driver/agent"
)

type Options struct {
	// Server は gorviz-server が agent を待ち受けるアドレス (gorviz-server の -agent-addr)
	Server string
	// Name は agent の名前。server はこの名前で束ねる
	Name string
	// Args は gorviz-agent と同じフラグ (例: "-dump-interval", "30s")。省いたものは既定値
	Args []string
}

type Agent struct {
	agent  *agent.Agent
	client *inprocess.RuntimeClient
	logs   *io.PipeReader
	logsW  *io.PipeWriter
}

func New(opt Options) (*Agent, error) {
	cfg, err := config.NewConfig("gorvizagent", opt.Args)
	if err != nil {
		return nil, err
	}
	cfg.AgentAddr, cfg.Name, cfg.TargetURL = opt.Server, opt.Name, "in-process"
	c, err := inprocess.NewRuntimeClient(cfg.ChunkDuration, uint64(min(cfg.TraceBufferBytes, 64<<20)))
	if err != nil {
		return nil, err
	}
	r, w := io.Pipe()
	return &Agent{agent: agent.New(cfg, c, schedtrace.NewStore()), client: c, logs: r, logsW: w}, nil
}

// LogWriter はアプリのログを agent にも渡すための書き込み先。1 行 1 レコード (slog の JSON / テキストなど) で書く
func (a *Agent) LogWriter() io.Writer {
	return a.logsW
}

// Run は ctx が終わるまで、録画と server へのつなぎを回す
func (a *Agent) Run(ctx context.Context) error {
	defer a.client.Stop()
	context.AfterFunc(ctx, func() { a.logsW.Close() })
	return a.agent.Run(ctx, agent.LogSource{R: a.logs, Source: "app", Echo: io.Discard})
}
