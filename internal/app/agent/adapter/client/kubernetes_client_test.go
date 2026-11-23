package client_test

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/client"
	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
)

const podsJSON = `{"items":[{
  "metadata":{"uid":"u1","name":"api-1","namespace":"shop","labels":{"app":"api"},"annotations":{"gorviz/pprof-port":"6060"}},
  "spec":{"containers":[{"name":"app"}]},
  "status":{"phase":"Running","podIP":"10.0.0.5","containerStatuses":[{"name":"app","containerID":"containerd://abc"}]}
}]}`

func TestKubernetesClient_ListNodePods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    []domain.Pod
		wantErr error
	}{
		{
			name: "lists the pods on the node with the service account token",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v1/pods", r.URL.Path)
				assert.Equal(t, "spec.nodeName=node-1", r.URL.Query().Get("fieldSelector"))
				assert.Equal(t, "Bearer token-1", r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(podsJSON))
			},
			want: []domain.Pod{{
				UID: "u1", Namespace: "shop", Name: "api-1", IP: "10.0.0.5", Running: true,
				Labels: map[string]string{"app": "api"}, Annotations: map[string]string{"gorviz/pprof-port": "6060"},
				Containers: []string{"app"}, ContainerIDs: map[string]string{"app": "containerd://abc"},
			}},
		},
		{
			name:    "reports a refused request as internal",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "forbidden", http.StatusForbidden) },
			wantErr: domain.ErrInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewTLSServer(tt.handler)
			defer srv.Close()
			dir := t.TempDir()
			tokenFile, caFile := filepath.Join(dir, "token"), filepath.Join(dir, "ca.crt")
			require.NoError(t, os.WriteFile(tokenFile, []byte("token-1\n"), 0o600))
			require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
			c, err := client.NewKubernetesClient(&config.Config{KubernetesAPI: srv.URL, NodeName: "node-1", KubernetesToken: tokenFile, KubernetesCAFile: caFile, RequestTimeout: 5 * time.Second})
			require.NoError(t, err)

			got, err := c.ListNodePods(ctx)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
