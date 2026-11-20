package agentrpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentkvs "github.com/kakkky/gorviz/internal/app/agent/adapter/kvs"
	agentrecording "github.com/kakkky/gorviz/internal/app/agent/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/agent/driver/serverconn"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/push"
	agentusecase "github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/app/protocol/agentrpc"
	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/adapter/agentconn"
	serverrecording "github.com/kakkky/gorviz/internal/app/server/adapter/recording"
	"github.com/kakkky/gorviz/internal/app/server/driver/agentlistener"
	"github.com/kakkky/gorviz/internal/app/server/presentation/ingest"
	serverusecase "github.com/kakkky/gorviz/internal/app/server/usecase"
)

// fakeRuntime は dump に gs を返す RuntimeClient。trace は録画に直接積むので取らない
type fakeRuntime struct{ gs []model.Goroutine }

func (f fakeRuntime) FetchGoroutines(context.Context) ([]model.Goroutine, error) { return f.gs, nil }

func (f fakeRuntime) FetchGoroutinesWithLeakCheck(context.Context) ([]model.Goroutine, error) {
	return f.gs, nil
}

func (f fakeRuntime) FetchTraceChunk(context.Context, time.Duration) (model.TraceChunk, error) {
	return model.TraceChunk{}, nil
}

// server は区切りをリングバッファに、ログ・dump を inbox に受ける
type server struct {
	inbox  *agentconn.Inbox
	traces *serverrecording.TraceStore
}

func startServer(ctx context.Context, addr string, accepted chan<- agentrpc.Hello) server {
	s := server{inbox: agentconn.NewInbox(100), traces: serverrecording.NewTraceStore(1 << 20)}
	uc := serverusecase.NewUseCase(serverusecase.Settings{}, s.inbox, s.inbox, nil, s.traces, nil, nil, nil, nil, s.inbox)
	go func() {
		_ = agentlistener.Accept(ctx, addr, func(h agentrpc.Hello) (agentlistener.Endpoint, bool) {
			if accepted != nil {
				accepted <- h
			}
			return agentlistener.Endpoint{Ingest: ingest.NewService(uc), Attach: s.inbox.Attach}, true
		})
	}()
	return s
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func TestAgentToServer(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := freeAddr(t)

	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	traces := agentrecording.NewTraceStore(1 << 20)
	for i := range 6 {
		traces.Append(model.TraceChunk{StartedAt: base.Add(time.Duration(i) * time.Second), Timeline: model.SchedulerTimeline{Window: time.Second}, LogMarks: []model.TraceLogMark{{Seq: uint64(i), Goroutine: 9}}})
	}
	goroutines := []model.Goroutine{{ID: 1, State: "chan receive", Labels: map[string]string{"scope": "s"}}}
	logs := agentrecording.NewLogStore(100)
	logs.Append(model.LogLine{Message: "a"})
	logs.Append(model.LogLine{Message: "b"})
	agentUC := agentusecase.NewUseCase(agentusecase.Settings{}, fakeRuntime{goroutines}, agentkvs.NewInMemoryKVS(), traces, logs, nil)
	_, err := agentUC.NewRecordDumpUsecase().Exec(ctx, agentusecase.RecordDumpUsecaseInput{Now: base})
	require.NoError(t, err)

	accepted := make(chan agentrpc.Hello, 1)
	srv := startServer(ctx, addr, accepted)
	go func() {
		_ = serverconn.Push(ctx, addr, agentrpc.Hello{Name: "api", Target: "http://app:6060"}, push.NewStream(10*time.Millisecond, agentUC).Run)
	}()

	select {
	case h := <-accepted:
		assert.Equal(t, "api", h.Name, "the agent introduces itself on the connection it dialed")
	case <-time.After(5 * time.Second):
		t.Fatal("the agent did not connect")
	}
	require.Eventually(t, srv.inbox.Connected, 5*time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool { _, ok := srv.inbox.LatestDump(); return ok }, 5*time.Second, 10*time.Millisecond)
	got, _ := srv.inbox.LatestDump()
	assert.Equal(t, goroutines, got.Goroutines, "the agent pushes the dump")
	assert.Equal(t, []model.LogLine{{ID: 2, Message: "b"}}, srv.inbox.LogsSince(1, 10), "the server reads the pushed logs after its cursor")
	assert.Empty(t, srv.inbox.LogsSince(2, 10), "lines the server has read are dropped from the inbox")

	require.Eventually(t, func() bool { return len(srv.traces.Between(base, base.Add(time.Hour))) == 6 }, 5*time.Second, 10*time.Millisecond,
		"every chunk reaches the server, a few at a time")
	_, mark, ok := srv.traces.FindLogMark(3)
	assert.True(t, ok)
	assert.Equal(t, model.GoroutineID(9), mark.Goroutine, "chunks keep the log marks")
}

func TestAgentReconnectsAfterServerRestart(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := freeAddr(t)

	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	traces := agentrecording.NewTraceStore(1 << 20)
	traces.Append(model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{Window: time.Second}})
	agentUC := agentusecase.NewUseCase(agentusecase.Settings{}, fakeRuntime{}, agentkvs.NewInMemoryKVS(), traces, agentrecording.NewLogStore(100), nil)
	_, err := agentUC.NewRecordDumpUsecase().Exec(ctx, agentusecase.RecordDumpUsecaseInput{Now: base})
	require.NoError(t, err)
	go func() {
		_ = serverconn.Push(ctx, addr, agentrpc.Hello{Name: "api", Target: "http://app:6060"}, push.NewStream(10*time.Millisecond, agentUC).Run)
	}()

	firstCtx, stopFirst := context.WithCancel(ctx)
	first := startServer(firstCtx, addr, nil)
	require.Eventually(t, first.inbox.Connected, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { _, ok := first.traces.Latest(); return ok }, 5*time.Second, 10*time.Millisecond)

	stopFirst()
	second := startServer(ctx, addr, nil)

	require.Eventually(t, func() bool { _, ok := second.traces.Latest(); return ok }, 10*time.Second, 50*time.Millisecond, "the agent sends the chunks it holds to the restarted server")
	require.Eventually(t, func() bool { _, ok := second.inbox.LatestDump(); return ok }, 10*time.Second, 50*time.Millisecond, "the agent pushes to the restarted server again")
}
