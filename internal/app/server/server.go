// Package server は gorviz-server を組み立てる。GUI・API の HTTP と、agent からの接続の待ち受けを回し、
// つながってきた agent ごとに診断・保存・画面を 1 つの Session に組み立てる
package server

import (
	"context"
	"path/filepath"
	"time"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/protocol/agentrpc"
	"github.com/kakkky/gorviz/internal/app/server/adapter/agentconn"
	"github.com/kakkky/gorviz/internal/app/server/adapter/issuestore"
	"github.com/kakkky/gorviz/internal/app/server/adapter/kvs"
	"github.com/kakkky/gorviz/internal/app/server/adapter/metricstore"
	"github.com/kakkky/gorviz/internal/app/server/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/agentlistener"
	"github.com/kakkky/gorviz/internal/app/server/driver/agentsession"
	"github.com/kakkky/gorviz/internal/app/server/driver/httpserver"
	"github.com/kakkky/gorviz/internal/app/server/presentation/handler"
	"github.com/kakkky/gorviz/internal/app/server/presentation/ingest"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/app/server/worker"
)

// Run は GUI・API の HTTP と、agent からの接続の待ち受けを回す。ctx が終わるまで返らない
func Run(ctx context.Context, cfg *config.Config) error {
	registry := agentsession.NewRegistry(func(h agentrpc.Hello) (*agentsession.Session, error) { return newSession(cfg, h) })
	srv := httpserver.NewHTTPServer(cfg, httpserver.NewMux(handler.NewAgentRouter(registry).Routes()))
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(srv.Run)
		sc.Go(func(ctx context.Context) error {
			return agentlistener.Accept(ctx, cfg.AgentAddr, func(h agentrpc.Hello) (agentlistener.Endpoint, bool) {
				return registry.Attach(ctx, h, func(f func(context.Context) error) { sc.Go(f) })
			})
		})
		return nil
	})
}

// newSession は agent 1 つ分の診断・保存・画面を組み立てる
func newSession(cfg *config.Config, hello agentrpc.Hello) (*agentsession.Session, error) {
	saveDir := ""
	if cfg.SaveDir != "" {
		saveDir = filepath.Join(cfg.SaveDir, hello.Name)
	}
	saved, err := recording.NewSavedStore(saveDir, time.Now())
	if err != nil {
		return nil, err
	}
	metricsDir := ""
	if saveDir != "" {
		metricsDir = filepath.Join(saveDir, "metrics")
	}
	metrics, err := metricstore.NewStore(metricsDir, time.Now())
	if err != nil {
		return nil, err
	}
	issuesDir := ""
	if saveDir != "" {
		issuesDir = filepath.Join(saveDir, "issues")
	}
	issues, err := issuestore.NewStore(issuesDir, time.Now())
	if err != nil {
		return nil, err
	}
	inbox := agentconn.NewInbox(cfg.MaxLogLines)
	sessionCfg := *cfg
	sessionCfg.TargetURL = hello.Name + " — " + hello.Target
	uc := usecase.NewUseCase(settingsOf(cfg), inbox, inbox, kvs.NewInMemoryKVS(),
		recording.NewCompositeTraceRecording(recording.NewTraceStore(cfg.TraceBufferBytes), saved), recording.NewLogStore(cfg.MaxLogLines), saved, metrics, issues, inbox)
	return &agentsession.Session{
		Handler: handler.Session{
			Name: hello.Name, Service: hello.Service, Target: hello.Target, Connected: inbox.Connected, UseCase: uc,
			Handler: httpserver.NewRouteMux(handler.NewHandler(&sessionCfg, uc).Routes()),
		},
		Endpoint: agentlistener.Endpoint{Ingest: ingest.NewService(uc), Attach: inbox.Attach},
		Workers:  []func(context.Context) error{worker.NewDiagnoseWorker(hello.Name, inbox.Connected, cfg.Refresh, uc).Run},
	}, nil
}

func settingsOf(cfg *config.Config) usecase.Settings {
	return usecase.Settings{
		TraceWindow: cfg.TraceWindow,
		HistorySize: cfg.HistorySize,
		Issue: domain.IssuePolicy{
			CPUWaitRatio: cfg.CPUWaitRatio,
			GCPerSecond:  cfg.GCPerSecond,
		},
		Leak: domain.LeakDetectionPolicy{
			MinSamples: cfg.LeakMinSamples,
			MinGrowth:  cfg.LeakMinGrowth,
		},
	}
}
