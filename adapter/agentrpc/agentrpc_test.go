package agentrpc_test

import (
	"context"
	"net"
	"net/rpc"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/adapter/agentrpc"
	"github.com/kakkky/gorviz/adapter/recording"
	"github.com/kakkky/gorviz/adapter/schedtrace"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
)

type fakeDumps struct{ dump domain.GoroutineDump }

func (f fakeDumps) LatestDump() (domain.GoroutineDump, bool) { return f.dump, true }

type fakeLogs struct{ lines []domain.LogLine }

func (f fakeLogs) LogsSince(after domain.LogLineID, limit int) []domain.LogLine {
	var out []domain.LogLine
	for _, l := range f.lines {
		if l.ID > after && len(out) < limit {
			out = append(out, l)
		}
	}
	return out
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
	traces := recording.NewTraceStore(&config.Config{TraceBufferBytes: 1 << 20})
	traces.Append(domain.TraceChunk{StartedAt: base, Timeline: domain.SchedulerTimeline{Window: time.Second}, Raw: []byte("raw"), LogMarks: []domain.TraceLogMark{{Seq: 3, Goroutine: 9}}})
	dump := domain.GoroutineDump{At: base, Goroutines: []domain.Goroutine{{ID: 1, State: "chan receive", Labels: map[string]string{"scope": "s"}}}, LeakCheck: domain.LeakCheckDone}
	svc := agentrpc.NewService(traces, fakeDumps{dump}, fakeLogs{[]domain.LogLine{{ID: 1, Message: "a"}, {ID: 2, Message: "b"}}}, schedtrace.NewStore())

	accepted := make(chan agentrpc.Hello, 1)
	agent := agentrpc.NewClient()
	go func() {
		_ = agentrpc.Accept(ctx, addr, func(h agentrpc.Hello, c *rpc.Client) {
			agent.SetConn(c)
			accepted <- h
		})
	}()
	go func() { _ = agentrpc.Dial(ctx, addr, agentrpc.Hello{Name: "api", Target: "http://app:6060"}, svc) }()

	select {
	case h := <-accepted:
		assert.Equal(t, agentrpc.Hello{Name: "api", Target: "http://app:6060"}, h, "the agent introduces itself on the connection it dialed")
	case <-time.After(5 * time.Second):
		t.Fatal("the agent did not connect")
	}

	got, ok := agent.LatestDump()
	assert.True(t, ok)
	assert.Equal(t, dump.Goroutines, got.Goroutines, "the server pulls the dump over the agent's connection")
	assert.Equal(t, []domain.LogLine{{ID: 2, Message: "b"}}, agent.LogsSince(1, 10))

	latest, ok := agent.Latest()
	assert.True(t, ok)
	assert.Nil(t, latest.Raw, "the latest chunk for the live view is sent without the raw trace")
	again, ok := agent.Latest()
	assert.True(t, ok)
	assert.Equal(t, latest.StartedAt, again.StartedAt, "an unchanged latest chunk is not sent again")

	between := agent.Between(base, base.Add(time.Second))
	require.Len(t, between, 1)
	assert.Equal(t, []byte("raw"), between[0].Raw, "chunks for saving carry the raw trace")

	_, mark, ok := agent.FindLogMark(3)
	assert.True(t, ok)
	assert.Equal(t, domain.GoroutineID(9), mark.Goroutine)
}
