package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/client"
	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/flightrecorder"
)

func TestFlightRecorderClient(t *testing.T) {
	ctx := context.Background()
	fr, stop, err := flightrecorder.NewFlightRecorder(&config.Config{ChunkLength: 50 * time.Millisecond, FlightRecorderBytes: 4 << 20})
	require.NoError(t, err)
	defer stop()
	c, err := client.NewFlightRecorderClient(fr)
	require.NoError(t, err)
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "cuts the recent execution trace of the process after the chunk length",
			check: func(t *testing.T) {
				raw, err := c.FetchExecutionTrace(ctx, domain.TargetInstance{}, 50*time.Millisecond)
				require.NoError(t, err)
				p, err := domain.ParseExecutionTrace(raw.Data, raw.ReceivedAt, time.Time{})
				require.NoError(t, err)
				assert.Positive(t, p.Chunk.Duration)
			},
		},
		{
			name: "counts the threads from the threadcreate profile",
			check: func(t *testing.T) {
				profile, err := c.FetchThreadCreateProfile(ctx, domain.TargetInstance{})
				require.NoError(t, err)
				n, err := domain.ParseThreadCount(profile)
				require.NoError(t, err)
				assert.Positive(t, n)
			},
		},
		{
			name: "takes the goroutineleak profile of the process",
			check: func(t *testing.T) {
				profile, err := c.FetchGoroutineLeakProfile(ctx, domain.TargetInstance{})
				require.NoError(t, err)
				_, err = domain.ParseGoroutineLeakProfile(profile, time.Now())
				require.NoError(t, err)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t)
		})
	}
}
