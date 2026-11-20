// Package domain は CLI の側の口 (server の API)。判定は持たない (server の domain が持つ)
package domain

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/protocol/serverapi"
)

// Server は server の API を agent 1 つ分に絞って呼ぶ口
type Server interface {
	Issues(ctx context.Context) ([]serverapi.IssueJSON, error)
}
