package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/client"
	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

func TestPprofClient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		call    func(c *client.PprofClient, target domain.TargetInstance) ([]byte, error)
		want    string
		wantErr error
	}{
		{
			name: "takes the execution trace for the chunk length in seconds",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/debug/pprof/trace", r.URL.Path)
				assert.Equal(t, "1.5", r.URL.Query().Get("seconds"))
				_, _ = w.Write([]byte("trace"))
			},
			call: func(c *client.PprofClient, target domain.TargetInstance) ([]byte, error) {
				raw, err := c.FetchExecutionTrace(ctx, target, 1500*time.Millisecond)
				if err == nil {
					assert.WithinDuration(t, time.Now(), raw.ReceivedAt, time.Second)
				}
				return raw.Data, err
			},
			want: "trace",
		},
		{
			name: "takes the goroutineleak profile as counts per stack",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/debug/pprof/goroutineleak", r.URL.Path)
				assert.Equal(t, "1", r.URL.Query().Get("debug"))
				_, _ = w.Write([]byte("goroutineleak profile: total 0\n"))
			},
			call: func(c *client.PprofClient, target domain.TargetInstance) ([]byte, error) {
				return c.FetchGoroutineLeakProfile(ctx, target)
			},
			want: "goroutineleak profile: total 0\n",
		},
		{
			name:    "reports a profile the app does not serve as not found",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "Unknown profile", http.StatusNotFound) },
			call: func(c *client.PprofClient, target domain.TargetInstance) ([]byte, error) {
				return c.FetchGoroutineLeakProfile(ctx, target)
			},
			wantErr: domain.ErrNotFound,
		},
		{
			name:    "reports other failures as internal",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
			call: func(c *client.PprofClient, target domain.TargetInstance) ([]byte, error) {
				return c.FetchThreadCreateProfile(ctx, target)
			},
			wantErr: domain.ErrInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			c, err := client.NewPprofClient(&config.Config{RequestTimeout: 5 * time.Second})
			require.NoError(t, err)

			got, err := tt.call(c, domain.TargetInstance{PprofAddr: strings.TrimPrefix(srv.URL, "http://")})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}
