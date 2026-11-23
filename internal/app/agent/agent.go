package agent

import (
	"context"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/agent/adapter/client"
	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/driver/flightrecorder"
	"github.com/kakkky/gorviz/internal/app/agent/driver/grpcconn"
	"github.com/kakkky/gorviz/internal/app/agent/driver/kvs"
	"github.com/kakkky/gorviz/internal/app/agent/driver/logsource"
	"github.com/kakkky/gorviz/internal/app/agent/driver/ringbuffer"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/logs"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/app/agent/worker"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// Run は gorviz-agent (sidecar 型・daemon 型) を動かす。アプリの pprof から取り、ログはファイル・TCP・UDP・標準入力から取り込む
func Run(ctx context.Context, cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	// driver (adapter が使う接続)
	conn, closeConn, err := grpcconn.NewClientConn(cfg)
	if err != nil {
		return err
	}
	defer closeConn()
	store := kvs.NewInMemoryKVS()
	buf, err := ringbuffer.NewRingBuffer[observation.Instance](cfg)
	if err != nil {
		return err
	}

	// adapter
	kvsAccessor := accessor.NewKVSAccessor(store)
	bufAccessor := accessor.NewRingBufferAccessor(buf)
	pprof, err := client.NewPprofClient(cfg)
	if err != nil {
		return err
	}
	kubernetes, err := client.NewKubernetesClient(cfg)
	if err != nil {
		return err
	}
	server, err := client.NewGorvizServerClient(cfg, conn)
	if err != nil {
		return err
	}

	// usecase
	uc := usecase.NewUseCase(kvsAccessor, kvsAccessor, kvsAccessor, kvsAccessor, kvsAccessor, kvsAccessor, bufAccessor, pprof, kubernetes, server)

	// presentation
	receiver := logs.NewReceiver(cfg, uc.NewForwardLogLineUsecase())

	// worker
	syncWorker, err := worker.NewSyncInstancesWorker(cfg, uc.NewSyncInstancesUsecase())
	if err != nil {
		return err
	}
	observationWorker, err := worker.NewObservationWorker(cfg, uc.NewCollectObservationsUsecase())
	if err != nil {
		return err
	}
	exportWorker := worker.NewExportWorker(cfg, uc.NewExportObservationsUsecase())

	// driver (presentation を受けて待ち受ける)
	fileTailer, err := logsource.NewFileTailer(cfg, receiver)
	if err != nil {
		return err
	}
	tcpListener, closeTCP, err := logsource.NewTCPListener(cfg, receiver)
	if err != nil {
		return err
	}
	defer closeTCP()
	udpListener, closeUDP, err := logsource.NewUDPListener(cfg, receiver)
	if err != nil {
		return err
	}
	defer closeUDP()
	stdinReader, err := logsource.NewStdinReader(cfg, receiver)
	if err != nil {
		return err
	}

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(syncWorker.Run)
		sc.Go(observationWorker.Run)
		sc.Go(exportWorker.Run)
		sc.Go(fileTailer.Run)
		sc.Go(tcpListener.Run)
		sc.Go(udpListener.Run)
		sc.Go(stdinReader.Run)
		return nil
	})
}

// RunEmbedded はライブラリ型の agent をアプリのプロセスの中で動かす。gorviz パッケージだけが呼ぶ。
// trace を runtime の FlightRecorder から取り、ログをロガーアダプタから受けるので、Run とは組み立てる driver と adapter が違う
func RunEmbedded(ctx context.Context, cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	// driver (adapter が使う接続)
	conn, closeConn, err := grpcconn.NewClientConn(cfg)
	if err != nil {
		return err
	}
	defer closeConn()
	fr, stopRecorder, err := flightrecorder.NewFlightRecorder(cfg)
	if err != nil {
		return err
	}
	defer stopRecorder()
	store := kvs.NewInMemoryKVS()
	buf, err := ringbuffer.NewRingBuffer[observation.Instance](cfg)
	if err != nil {
		return err
	}

	// adapter
	kvsAccessor := accessor.NewKVSAccessor(store)
	bufAccessor := accessor.NewRingBufferAccessor(buf)
	runtime, err := client.NewFlightRecorderClient(fr)
	if err != nil {
		return err
	}
	server, err := client.NewGorvizServerClient(cfg, conn)
	if err != nil {
		return err
	}

	// usecase
	uc := usecase.NewUseCase(kvsAccessor, kvsAccessor, kvsAccessor, kvsAccessor, kvsAccessor, kvsAccessor, bufAccessor, runtime, nil, server)

	// presentation
	receiver := logs.NewReceiver(cfg, uc.NewForwardLogLineUsecase())

	// worker
	syncWorker, err := worker.NewSyncInstancesWorker(cfg, uc.NewSyncInstancesUsecase())
	if err != nil {
		return err
	}
	observationWorker, err := worker.NewObservationWorker(cfg, uc.NewCollectObservationsUsecase())
	if err != nil {
		return err
	}
	exportWorker := worker.NewExportWorker(cfg, uc.NewExportObservationsUsecase())

	// driver (presentation を受けて待ち受ける)
	hook, unhook, err := logsource.NewLoggerHook(cfg, receiver)
	if err != nil {
		return err
	}
	defer unhook()

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(syncWorker.Run)
		sc.Go(observationWorker.Run)
		sc.Go(exportWorker.Run)
		sc.Go(hook.Run)
		return nil
	})
}
