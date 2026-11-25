// Package server はアプリをビルドして起動し、負荷をかけ、agent から届いた execution trace から Issue を判定する。結果を GUI と HTTP API で返す
package server

import (
	"context"
	"net/http"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal"
	"github.com/kakkky/gorviz/internal/app/server/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/server/adapter/client"
	"github.com/kakkky/gorviz/internal/app/server/assets"
	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/driver/httpserver"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
	socketdriver "github.com/kakkky/gorviz/internal/app/server/driver/socket"
	"github.com/kakkky/gorviz/internal/app/server/driver/tracestream"
	"github.com/kakkky/gorviz/internal/app/server/driver/workdir"
	"github.com/kakkky/gorviz/internal/app/server/presentation/api"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	socketpresentation "github.com/kakkky/gorviz/internal/app/server/presentation/socket"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/app/server/worker"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

func Run(ctx context.Context, cfg *config.Config) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	// driver (adapter が使う置き場・接続)
	work, removeWork, err := workdir.NewWorkDir()
	if err != nil {
		return err
	}
	defer removeWork()
	store := kvs.NewInMemoryKVS()
	stream := tracestream.NewTraceStream()
	conn := socketdriver.NewSocketConn()
	agentSource, socketSource, err := internal.AgentSource()
	if err != nil {
		return err
	}

	// adapter
	sessions := accessor.NewSessionAccessor(store)
	index := accessor.NewTraceIndexAccessor(store)
	issues := accessor.NewIssueBookAccessor(store)
	traceStream := accessor.NewTraceStreamAccessor(stream)
	sources := accessor.NewAgentSourceAccessor(agentSource, socketSource)
	agent, err := client.NewAgentClient(conn)
	if err != nil {
		return err
	}
	app, err := client.NewAppClient(work.SocketPath(), work.Path())
	if err != nil {
		return err
	}
	toolchain, err := client.NewToolchainClient(work.Path())
	if err != nil {
		return err
	}
	clock, err := client.NewClockClient()
	if err != nil {
		return err
	}

	// usecase
	uc := usecase.NewUseCase(sessions, index, issues, traceStream, agent, app, app, app, toolchain, sources, clock)

	// presentation
	gui := "http://" + cfg.Addr
	socketHandler := socketpresentation.NewSocketHandler(uc.NewIngestAppObservationsUsecase())
	mux := http.NewServeMux()
	mux.Handle("GET "+serverapi.PathStatus, api.NewGetStatusHandler(uc.NewGetSessionStatusUsecase(), gui))
	mux.Handle("GET "+serverapi.PathStartResult, api.NewGetStartResultHandler(uc.NewWaitAppStartUsecase(), stop, gui))
	mux.Handle("POST "+serverapi.PathDiagnose, api.NewPostDiagnoseHandler(uc.NewDiagnoseAppUsecase(), gui))
	mux.Handle("GET "+serverapi.PathIssues, api.NewGetIssuesHandler(uc.NewListIssuesUsecase(), gui))
	mux.Handle("GET "+serverapi.PathIssue, api.NewGetIssueHandler(uc.NewGetIssueUsecase(), gui))
	mux.Handle("GET "+serverapi.PathGoroutine, api.NewGetGoroutineHandler(uc.NewGetGoroutineDetailUsecase(), gui))
	mux.Handle("POST "+serverapi.PathStop, api.NewPostStopHandler(uc.NewWaitAppShutdownUsecase(), stop))
	mux.Handle("GET /{$}", web.NewGetRootHandler())
	mux.Handle("GET /overview", web.NewGetOverviewHandler(uc.NewGetSessionStatusUsecase(), uc.NewGetOverviewUsecase()))
	mux.Handle("GET /issues", web.NewGetIssuesHandler(uc.NewGetSessionStatusUsecase(), uc.NewListIssuesUsecase()))
	mux.Handle("GET /issues/{id}", web.NewGetIssueHandler(uc.NewGetSessionStatusUsecase(), uc.NewGetIssueUsecase()))
	mux.Handle("GET /scheduler", web.NewGetSchedulerHandler(uc.NewGetSessionStatusUsecase(), uc.NewGetSchedulerWindowUsecase()))
	mux.Handle("GET /scheduler/stream", web.NewGetSchedulerStreamHandler(uc.NewGetSchedulerWindowUsecase()))
	mux.Handle("GET /goroutines/{id}", web.NewGetGoroutineHandler(uc.NewGetSessionStatusUsecase(), uc.NewGetGoroutineDetailUsecase(), uc.NewGetSchedulerWindowUsecase()))
	mux.Handle("GET "+assets.URLPrefix, web.NewAssetsHandler())
	mw := middleware.NewMiddleware(api.NewErrorWriter(), web.NewErrorWriter())

	// worker
	runAppWorker := worker.NewRunAppWorker(cfg, uc.NewRunAppUsecase())
	indexWorker := worker.NewIndexAppRuntimeTraceWorker(uc.NewIndexAppRuntimeTraceUsecase())
	collectWorker := worker.NewCollectAppObservationsWorker(uc.NewCollectAppObservationsUsecase())
	detectWorker := worker.NewDetectAppIssuesWorker(uc.NewDetectAppIssuesUsecase())

	// driver (presentation を受けて待ち受ける)
	httpServer, closeHTTP, err := httpserver.NewHTTPServer(cfg.Addr, mw.Wrap(mux))
	if err != nil {
		return err
	}
	defer closeHTTP()
	listener, closeListener, err := socketdriver.NewSocketListener(work.SocketPath(), conn, socketHandler)
	if err != nil {
		return err
	}
	defer closeListener()

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(listener.Run)
		sc.Go(httpServer.Run)
		sc.Go(runAppWorker.Run)
		sc.Go(indexWorker.Run)
		sc.Go(collectWorker.Run)
		sc.Go(detectWorker.Run)
		return nil
	})
}
