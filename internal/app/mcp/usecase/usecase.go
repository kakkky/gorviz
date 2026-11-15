package usecase

import "github.com/kakkky/gorviz/internal/app/mcp/domain"

// UseCase は MCP の側。ツールごとに、指定された agent の server の API から取って返す
type UseCase struct {
	serverFor domain.ServerFor
}

func NewUseCase(serverFor domain.ServerFor) *UseCase {
	return &UseCase{serverFor: serverFor}
}
