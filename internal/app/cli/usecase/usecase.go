package usecase

import "github.com/kakkky/gorviz/internal/app/cli/domain"

// UseCase は CLI の側。server の API から取ったものを返す
type UseCase struct {
	server domain.Server
}

func NewUseCase(server domain.Server) *UseCase {
	return &UseCase{server: server}
}
