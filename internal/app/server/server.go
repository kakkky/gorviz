// Package server は gorviz-server を組み立てる
package server

import (
	"context"
	"net/http"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/server/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/server/adapter/client"
	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/authtoken"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/app/server/driver/grpcserver"
	"github.com/kakkky/gorviz/internal/app/server/driver/httpserver"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
	"github.com/kakkky/gorviz/internal/app/server/driver/tlscert"
	"github.com/kakkky/gorviz/internal/app/server/presentation/api"
	"github.com/kakkky/gorviz/internal/app/server/presentation/interceptor"
	"github.com/kakkky/gorviz/internal/app/server/presentation/login"
	"github.com/kakkky/gorviz/internal/app/server/presentation/middleware"
	"github.com/kakkky/gorviz/internal/app/server/presentation/route"
	"github.com/kakkky/gorviz/internal/app/server/presentation/rpc"
	"github.com/kakkky/gorviz/internal/app/server/presentation/web"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
	"github.com/kakkky/gorviz/internal/app/server/worker"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

func Run(ctx context.Context, cfg *config.Config) error {
	// driver (adapter が使う保存先・メモリの置き場)
	root, cleanup, err := filesystem.NewRoot(cfg.DataDir)
	if err != nil {
		return err
	}
	defer cleanup()
	store := kvs.NewInMemoryKVS()
	tokens, err := authtoken.NewTokens(root, cfg.BootstrapToken)
	if err != nil {
		return err
	}
	tlsConf, err := tlscert.NewTLSConfig(cfg, root)
	if err != nil {
		return err
	}

	// adapter
	holdings := accessor.NewHoldingAccessor(root, store)
	webhook, err := client.NewWebhookClient()
	if err != nil {
		return err
	}
	slack, err := client.NewSlackClient()
	if err != nil {
		return err
	}
	mail, err := client.NewMailClient()
	if err != nil {
		return err
	}

	// usecase
	uc := usecase.NewUseCase(
		usecase.Settings{
			Retention: domain.Retention{
				Metrics: cfg.RetentionMetrics, Issues: cfg.RetentionIssues, Logs: cfg.RetentionLogs, LogWriterGoroutines: cfg.RetentionLogWriterGoroutines,
				InstanceLifecycle: cfg.RetentionInstanceLifecycle, ExecutionTraces: cfg.RetentionExecutionTraces,
				ExecutionTracesAroundIssues: cfg.RetentionExecutionTracesAroundIssues, ExecutionTracesAroundErrorLogs: cfg.RetentionExecutionTracesAroundErrorLogs,
				GoroutineSnapshots: cfg.RetentionGoroutineSnapshots, GoroutineLeakChecks: cfg.RetentionGoroutineLeakChecks, GoroutineStateBreakdowns: cfg.RetentionGoroutineStateBreakdowns,
			},
			MaxDiskBytes:  cfg.MaxDiskBytes,
			GUI:           domain.GUI{Base: cfg.PublicURL},
			Notifications: cfg.Notifications,
		},
		holdings, holdings,
		accessor.NewInventoryAccessor(root),
		accessor.NewLogAccessor(root),
		accessor.NewTraceAccessor(root),
		accessor.NewSnapshotAccessor(root),
		accessor.NewLeakCheckAccessor(root),
		accessor.NewLifecycleAccessor(root),
		accessor.NewMetricAccessor(root),
		accessor.NewBreakdownAccessor(root),
		accessor.NewIssueAccessor(root),
		accessor.NewPreservedTraceAccessor(root),
		accessor.NewLogWriterAccessor(root),
		accessor.NewCheckpointAccessor(root),
		accessor.NewDataFileAccessor(root),
		webhook, slack, mail,
	)

	// presentation
	gui := domain.GUI{Base: cfg.PublicURL}
	ic := interceptor.NewInterceptor(tokens)
	observationService := rpc.NewObservationService(uc.NewIngestObservationsUsecase())
	mw := middleware.NewMiddleware(tokens)
	apiHandlers := map[string]http.Handler{
		serverapi.ListServices.Name:        api.NewGetServicesHandler(uc.NewListServicesUsecase(), gui),
		serverapi.GetOverview.Name:         api.NewGetOverviewHandler(uc.NewGetOverviewUsecase(), gui),
		serverapi.ListIssues.Name:          api.NewGetIssuesHandler(uc.NewListIssuesUsecase(), gui),
		serverapi.GetIssue.Name:            api.NewGetIssueHandler(uc.NewGetIssueUsecase(), gui),
		serverapi.SearchLogs.Name:          api.NewGetLogsHandler(uc.NewSearchLogsUsecase()),
		serverapi.CountLogs.Name:           api.NewGetLogCountsHandler(uc.NewCountLogsUsecase()),
		serverapi.GetLog.Name:              api.NewGetLogHandler(uc.NewGetLogUsecase(), gui),
		serverapi.DiagnoseGoroutine.Name:   api.NewGetGoroutineDiagnosisHandler(uc.NewDiagnoseGoroutineUsecase(), gui),
		serverapi.ListStateChanges.Name:    api.NewGetGoroutineStateChangesHandler(uc.NewListGoroutineStateChangesUsecase(), gui),
		serverapi.SummarizeGoroutines.Name: api.NewGetGoroutineSummaryHandler(uc.NewSummarizeGoroutinesUsecase(), gui),
		serverapi.ListGoroutinesAt.Name:    api.NewGetGoroutinesHandler(uc.NewListGoroutinesAtUsecase()),
		serverapi.ListAvailableData.Name:   api.NewGetAvailableDataHandler(uc.NewListAvailableDataUsecase(), gui),
	}
	loginHandlers := route.Login{Get: login.NewGetLoginHandler(), Post: login.NewPostLoginHandler(cfg, tokens), PostLogout: login.NewPostLogoutHandler()}
	services, chunks := uc.NewListServicesUsecase(), uc.NewListTraceChunksUsecase()
	guiHandlers := route.GUI{
		Root:            web.NewGetRootHandler(services),
		Assets:          web.NewGetAssetsHandler(),
		Overview:        web.NewGetOverviewHandler(services, uc.NewGetOverviewUsecase()),
		OverviewStream:  web.NewGetOverviewStreamHandler(services, uc.NewGetOverviewUsecase()),
		Issues:          web.NewGetIssuesHandler(services, uc.NewListIssuesUsecase()),
		Issue:           web.NewGetIssueHandler(services, uc.NewGetIssueUsecase()),
		Logs:            web.NewGetLogsHandler(services, uc.NewSearchLogsUsecase(), uc.NewCountLogsUsecase(), uc.NewGetLogUsecase()),
		LogGoroutine:    web.NewGetLogGoroutineHandler(uc.NewGetLogUsecase()),
		Scheduler:       web.NewGetSchedulerHandler(services, chunks),
		SchedulerStream: web.NewGetSchedulerStreamHandler(chunks),
		Goroutine:       web.NewGetGoroutineHandler(services, uc.NewDiagnoseGoroutineUsecase(), chunks),
	}
	mux, err := route.NewMux(mw, apiHandlers, loginHandlers, guiHandlers)
	if err != nil {
		return err
	}

	// worker
	aggregateMetricsWorker := worker.NewAggregateMetricsWorker(cfg, uc.NewAggregateMetricsUsecase())
	aggregateBreakdownsWorker := worker.NewAggregateGoroutineStateBreakdownsWorker(cfg, uc.NewAggregateGoroutineStateBreakdownsUsecase())
	detectIssuesWorker := worker.NewDetectIssuesWorker(cfg, uc.NewDetectIssuesUsecase())
	preserveWorker := worker.NewPreserveExecutionTracesAroundErrorLogsWorker(cfg, uc.NewPreserveExecutionTracesAroundErrorLogsUsecase())
	attachWorker := worker.NewAttachLogWriterGoroutinesWorker(cfg, uc.NewAttachLogWriterGoroutinesUsecase())
	pruneWorker := worker.NewPruneDataWorker(cfg, uc.NewPruneDataUsecase())

	// driver (presentation を受けて待ち受ける)
	grpcServer, closeGRPC, err := grpcserver.NewGRPCServer(cfg, tlsConf, ic.Unary, observationService)
	if err != nil {
		return err
	}
	defer closeGRPC()
	httpServer, closeHTTP, err := httpserver.NewHTTPServer(cfg, tlsConf, mux)
	if err != nil {
		return err
	}
	defer closeHTTP()

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(aggregateMetricsWorker.Run)
		sc.Go(aggregateBreakdownsWorker.Run)
		sc.Go(detectIssuesWorker.Run)
		sc.Go(preserveWorker.Run)
		sc.Go(attachWorker.Run)
		sc.Go(pruneWorker.Run)
		sc.Go(grpcServer.Run)
		sc.Go(httpServer.Run)
		return nil
	})
}
