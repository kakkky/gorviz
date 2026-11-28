// Package server はアプリをビルドして起動し、負荷をかけ、アプリの pprof から取った execution trace から Issue を判定する。結果を GUI と HTTP API で返す
package server

import (
	"context"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/server/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/server/adapter/client"
	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/driver/httpserver"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
	"github.com/kakkky/gorviz/internal/app/server/driver/tracestream"
	"github.com/kakkky/gorviz/internal/app/server/driver/workdir"
	"github.com/kakkky/gorviz/internal/app/server/presentation/api"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
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

	// adapter
	sessions := accessor.NewSessionAccessor(store)
	observations := accessor.NewObservationAccessor(store)
	issues := accessor.NewIssueBookAccessor(store)
	traceStream := accessor.NewTraceStreamAccessor(stream)
	app, err := client.NewAppClient(work.SocketPath(), work.Path())
	if err != nil {
		return err
	}
	clock, err := client.NewClockClient()
	if err != nil {
		return err
	}

	// usecase
	uc := usecase.NewUseCase(sessions, observations, issues, traceStream, app, app, app, app, clock)

	// presentation
	apiHandler := api.NewHandler(uc, "http://"+cfg.GUIAddr, cfg.ServerLog, stop)
	webHandler := web.NewHandler(uc)
	mw := middleware.NewMiddleware(api.NewErrorWriter(), web.NewErrorWriter())

	// worker
	runAppWorker := worker.NewRunAppWorker(cfg, uc.NewRunAppUsecase())
	fetchWorker := worker.NewFetchAppRuntimeTraceWorker(uc.NewFetchAppRuntimeTraceUsecase())
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

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(apiServer.Run)
		sc.Go(guiServer.Run)
		sc.Go(runAppWorker.Run)
		sc.Go(fetchWorker.Run)
		sc.Go(indexWorker.Run)
		sc.Go(collectWorker.Run)
		sc.Go(detectWorker.Run)
		return nil
	})
}
