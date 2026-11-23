//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_server.go -package=mock
package domain

import (
	"context"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

// ServerClient は観測データを送り、server がインスタンスごとにどこまで持っているかを受け取る
type ServerClient interface {
	Export(ctx context.Context, batches []observation.Batch) (map[observation.Instance]observation.Cursor, error)
}
