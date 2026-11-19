// Package domain は MCP の側の口 (server の API)。判定は持たない (server の domain が持つ)
package domain

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/protocol/serverapi"
)

// Server は server の API を agent 1 つ分に絞って呼ぶ口
type Server interface {
	Agents(ctx context.Context) ([]serverapi.AgentJSON, error)
	Overview(ctx context.Context) (serverapi.OverviewJSON, error)
	Issues(ctx context.Context) ([]serverapi.IssueJSON, error)
	Logs(ctx context.Context, q serverapi.LogQuery) ([]serverapi.LogJSON, error)
	Goroutine(ctx context.Context, id int64, at, from time.Time) (serverapi.GoroutineJSON, error)
	Recordings(ctx context.Context) ([]model.SavedRecording, error)
}

// ServerFor は agent を指定した Server を返す。"" なら起動時に決めた agent を使う
type ServerFor func(agent string) Server
