package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/client"
	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/grpcconn"
	"github.com/kakkky/gorviz/internal/domain/observation"
	"github.com/kakkky/gorviz/internal/protocol/agentrpc"
)

type fakeObservationService struct {
	agentrpc.UnimplementedObservationServiceServer
	export func([]observation.Batch) (map[observation.Instance]observation.Cursor, error)
}

func (f *fakeObservationService) Export(_ context.Context, req *agentrpc.ExportRequest) (*agentrpc.ExportResponse, error) {
	cursors, err := f.export(agentrpc.FromExportRequest(req))
	if err != nil {
		return nil, err
	}
	return agentrpc.ToExportResponse(cursors), nil
}

func TestGorvizServerClient_Export(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := observation.Instance{Service: "s", Name: "i", Incarnation: "1"}
	at := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	batch := observation.Batch{Instance: in, Logs: []observation.LogLine{{Agent: "a", Seq: 1, At: at, Raw: "hello"}}}
	tests := []struct {
		name    string
		export  func([]observation.Batch) (map[observation.Instance]observation.Cursor, error)
		want    map[observation.Instance]observation.Cursor
		wantErr error
	}{
		{
			name: "sends the batches and returns the server's cursors",
			export: func(b []observation.Batch) (map[observation.Instance]observation.Cursor, error) {
				assert.Equal(t, in, b[0].Instance)
				assert.Equal(t, "hello", b[0].Logs[0].Raw)
				return map[observation.Instance]observation.Cursor{in: {LogAgent: "a", LogSeq: 1}}, nil
			},
			want: map[observation.Instance]observation.Cursor{in: {LogAgent: "a", LogSeq: 1}},
		},
		{
			name: "reports a refused export as internal",
			export: func([]observation.Batch) (map[observation.Instance]observation.Cursor, error) {
				return nil, status.Error(codes.Unauthenticated, "unknown token")
			},
			wantErr: domain.ErrInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lis := bufconn.Listen(1 << 20)
			srv := grpc.NewServer()
			agentrpc.RegisterObservationServiceServer(srv, &fakeObservationService{export: tt.export})
			go func() { _ = srv.Serve(lis) }()
			defer srv.Stop()
			conn, err := grpc.NewClient("passthrough:///bufnet",
				grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
				grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			defer conn.Close()
			c, err := client.NewGorvizServerClient(&config.Config{RequestTimeout: 5 * time.Second}, &grpcconn.ClientConn{ClientConn: conn})
			require.NoError(t, err)

			got, err := c.Export(ctx, []observation.Batch{batch})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
