// Package agent はアプリのビルドに差し込まれ、アプリのプロセスの中で execution trace を server へ流す。
// 標準ライブラリと protocol/agent だけを import する
package agent

import (
	"context"
	"os"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/agent/adapter/client"
	socketdriver "github.com/kakkky/gorviz/internal/app/agent/driver/socket"
	"github.com/kakkky/gorviz/internal/app/agent/driver/tracer"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/function"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/middleware"
	socketpresentation "github.com/kakkky/gorviz/internal/app/agent/presentation/socket"
	"github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/app/agent/worker"
	"github.com/kakkky/gorviz/internal/protocol/agent"
)

var functionCallHandler *function.FunctionCallHandler

func init() { Start() }

// Start は server につながり、execution trace を取り始めてから戻る。server につながらなければ何もしない
func Start() {
	addr := os.Getenv(agent.EnvSocket)
	if addr == "" || functionCallHandler != nil {
		return
	}
	// driver (adapter が使う接続)
	conn, closeConn, err := socketdriver.NewSocketConn(addr)
	if err != nil {
		return
	}
	executionTracer, stopTracer := tracer.NewExecutionTracer()

	// adapter
	runtime := accessor.NewAppRuntimeAccessor(executionTracer)
	server, err := client.NewServerClient(conn)
	if err != nil {
		closeConn()
		return
	}

	// usecase
	uc := usecase.NewUseCase(runtime, runtime, runtime, runtime, server, server)

	// presentation
	handler := function.NewFunctionCallHandler(uc)
	socketHandler := socketpresentation.NewHandler(uc)
	mw := middleware.NewMiddleware(server)

	// worker
	streamWorker := worker.NewStreamAppRuntimeTraceWorker(uc.NewStreamAppRuntimeTraceUsecase())

	if err := handler.Start(); err != nil {
		stopTracer()
		closeConn()
		return
	}
	functionCallHandler = handler
	ctx := context.Background()
	go streamWorker.Run(ctx)
	go conn.Run(ctx, socketdriver.NewRequestMux(socketHandler, mw))
}

// Stop は最後の観測データを送り終え、接続を閉じてから戻る。アプリの main の先頭に overlay した defer から呼ばれる
func Stop() {
	if functionCallHandler == nil {
		return
	}
	functionCallHandler.Stop()
}
