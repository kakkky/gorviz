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
	"github.com/kakkky/gorviz/internal/app/agent/presentation/pull"
	"github.com/kakkky/gorviz/internal/app/agent/presentation/push"
	agentusecase "github.com/kakkky/gorviz/internal/app/agent/usecase"
	"github.com/kakkky/gorviz/internal/app/api/agentrpc"
	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/adapter/agentconn"
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

func TestAgentToServer(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	traces := agentrecording.NewTraceStore(1 << 20)
	traces.Append(model.TraceChunk{StartedAt: base, Timeline: model.SchedulerTimeline{Window: time.Second}, Raw: []byte("raw"), LogMarks: []model.TraceLogMark{{Seq: 3, Goroutine: 9}}})
	goroutines := []model.Goroutine{{ID: 1, State: "chan receive", Labels: map[string]string{"scope": "s"}}}
	logs := agentrecording.NewLogStore(100)
	logs.Append(model.LogLine{Message: "a"})
	logs.Append(model.LogLine{Message: "b"})
	agentUC := agentusecase.NewUseCase(agentusecase.Settings{}, fakeRuntime{goroutines}, agentkvs.NewInMemoryKVS(), traces, logs, nil)
	_, err = agentUC.NewRecordDumpUsecase().Exec(ctx, agentusecase.RecordDumpUsecaseInput{Now: base})
	require.NoError(t, err)

	hello := agentrpc.Hello{Name: "api", Target: "http://app:6060"}
	accepted := make(chan agentrpc.Hello, 2)
	agent := agentconn.NewClient(100)
	serverUC := serverusecase.NewUseCase(serverusecase.Settings{}, agent, agent, nil, agent, nil, nil, agent)
	go func() {
		_ = agentlistener.Accept(ctx, addr, func(h agentrpc.Hello) (agentlistener.Endpoint, bool) {
			accepted <- h
			return agentlistener.Endpoint{Pull: agent.SetConn, Ingest: ingest.NewService(serverUC)}, true
		})
	}()
	go func() { _ = serverconn.Push(ctx, addr, hello, push.NewStream(10*time.Millisecond, agentUC).Run) }()
	go func() { _ = serverconn.ServePull(ctx, addr, hello, pull.NewService(agentUC)) }()

	var kinds []agentrpc.ConnKind
	for range 2 {
		select {
		case h := <-accepted:
			assert.Equal(t, "api", h.Name, "the agent introduces itself on each connection it dialed")
			kinds = append(kinds, h.Kind)
		case <-time.After(5 * time.Second):
			t.Fatal("the agent did not connect")
		}
	}
	assert.ElementsMatch(t, []agentrpc.ConnKind{agentrpc.ConnPush, agentrpc.ConnPull}, kinds, "the agent dials one connection per direction")

	require.Eventually(t, func() bool { _, ok := agent.LatestDump(); return ok }, 5*time.Second, 10*time.Millisecond)
	got, _ := agent.LatestDump()
	assert.Equal(t, goroutines, got.Goroutines, "the agent pushes the dump")
	assert.Equal(t, []model.LogLine{{ID: 2, Message: "b"}}, agent.LogsSince(1, 10), "the server reads the pushed logs after its cursor")
	assert.Empty(t, agent.LogsSince(2, 10), "lines the server has read are dropped from the client")

	require.Eventually(t, agent.Connected, 5*time.Second, 10*time.Millisecond)
	latest, ok := agent.Latest()
	assert.True(t, ok)
	assert.Nil(t, latest.Raw, "the latest chunk for the live view is pulled without the raw trace")
	again, ok := agent.Latest()
	assert.True(t, ok)
	assert.Equal(t, latest.StartedAt, again.StartedAt, "an unchanged latest chunk is not sent again")

	between := agent.Between(base, base.Add(time.Second))
	require.Len(t, between, 1)
	assert.Equal(t, []byte("raw"), between[0].Raw, "chunks for saving carry the raw trace")

	_, mark, ok := agent.FindLogMark(3)
	assert.True(t, ok)
	assert.Equal(t, model.GoroutineID(9), mark.Goroutine)
}
