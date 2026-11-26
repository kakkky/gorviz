// Package server はアプリをビルドして起動し、負荷をかけ、agent から届いた execution trace から Issue を判定する。結果を GUI と HTTP API で返す
package server

import (
	"context"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal"
	"github.com/kakkky/gorviz/internal/app/server/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/server/adapter/client"
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
	agentFiles, protocolFiles, err := internal.EmbeddedAgent()
	if err != nil {
		return err
	}

	// adapter
	sessions := accessor.NewSessionAccessor(store)
	observations := accessor.NewObservationAccessor(store)
	issues := accessor.NewIssueBookAccessor(store)
	traceStream := accessor.NewTraceStreamAccessor(stream)
	agentClient, err := client.NewAgentClient(conn)
	if err != nil {
		return err
	}
	app, err := client.NewAppClient(work.SocketPath(), work.Path(), agentFiles, protocolFiles)
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
	uc := usecase.NewUseCase(sessions, observations, issues, traceStream, agentClient, app, app, app, toolchain, clock)

	// presentation
	socketHandler := socketpresentation.NewHandler(uc)
	apiHandler := api.NewHandler(uc, "http://"+cfg.GUIAddr, stop)
	webHandler := web.NewHandler(uc)
	mw := middleware.NewMiddleware(api.NewErrorWriter(), web.NewErrorWriter())

	// worker
	runAppWorker := worker.NewRunAppWorker(cfg, uc.NewRunAppUsecase())
	indexWorker := worker.NewIndexAppRuntimeTraceWorker(uc.NewIndexAppRuntimeTraceUsecase())
	collectWorker := worker.NewCollectAppObservationsWorker(uc.NewCollectAppObservationsUsecase())
	detectWorker := worker.NewDetectAppIssuesWorker(uc.NewDetectAppIssuesUsecase())

	// driver (presentation を受けて待ち受ける)
	apiServer, closeAPI, err := httpserver.NewUnixHTTPServer(cfg.Socket, httpserver.NewAPIMux(apiHandler, mw))
	if err != nil {
		return err
	}
	defer closeAPI()
	guiServer, closeGUI, err := httpserver.NewTCPHTTPServer(cfg.GUIAddr, httpserver.NewGUIMux(webHandler, mw))
	if err != nil {
		return err
	}
	defer closeGUI()
	listener, closeListener, err := socketdriver.NewSocketListener(work.SocketPath(), conn, socketdriver.NewMessageMux(socketHandler, mw))
	if err != nil {
		return err
	}
	defer closeListener()

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(listener.Run)
		sc.Go(apiServer.Run)
		sc.Go(guiServer.Run)
		sc.Go(runAppWorker.Run)
		sc.Go(indexWorker.Run)
		sc.Go(collectWorker.Run)
		sc.Go(detectWorker.Run)
		return nil
	})
}
