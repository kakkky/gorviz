package gorviz_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz"
)

func TestStart(t *testing.T) {
	a, err := gorviz.Start(context.Background(), gorviz.Options{Server: "127.0.0.1:1", Name: "test"})
	require.NoError(t, err)

	_, err = gorviz.Start(context.Background(), gorviz.Options{Server: "127.0.0.1:1"})
	assert.Error(t, err, "only one agent runs in a process")

	logger := slog.New(gorviz.NewSlogHandler(slog.NewJSONHandler(io.Discard, nil)))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 100_000 {
			logger.Info("request done", "i", i)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("logging is blocked by the embedded agent")
	}

	assert.NoError(t, a.Stop())
	again, err := gorviz.Start(context.Background(), gorviz.Options{Server: "127.0.0.1:1", Name: "test"})
	require.NoError(t, err, "an agent can start again after the previous one stopped")
	assert.NoError(t, again.Stop())
}
