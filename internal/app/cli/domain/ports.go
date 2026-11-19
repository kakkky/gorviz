// Package domain は CLI の側の口 (server の API と、録画の書き出し先)。判定は持たない (server の domain が持つ)
package domain

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/protocol/serverapi"
)

// Server は server の API を agent 1 つ分に絞って呼ぶ口
type Server interface {
	Issues(ctx context.Context) ([]serverapi.IssueJSON, error)
	Recordings(ctx context.Context) ([]model.SavedRecording, error)
	RecordingChunk(ctx context.Context, id string, n int) ([]byte, error)
}

// RecordingWriter は保存した録画の書き出し先
type RecordingWriter interface {
	// Create は out の下に録画 1 件の書き出し先を用意し、その場所を返す
	Create(out, id string) (string, error)
	// WriteChunk は Create で用意した場所に n 番目の区切りを書く
	WriteChunk(location string, n int, raw []byte) error
}
