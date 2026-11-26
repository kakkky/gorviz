package usecase

import "github.com/kakkky/gorviz/internal/app/agent/domain"

type UseCase struct {
	process    domain.AppRuntimeProcess
	trace      domain.AppRuntimeTrace
	coverage   domain.AppRuntimeCoverage
	goroutines domain.AppRuntimeGoroutines
	sender     domain.ServerSender
	connection domain.ServerConnection
}

func NewUseCase(process domain.AppRuntimeProcess, trace domain.AppRuntimeTrace, coverage domain.AppRuntimeCoverage, goroutines domain.AppRuntimeGoroutines, sender domain.ServerSender, connection domain.ServerConnection) *UseCase {
	return &UseCase{process: process, trace: trace, coverage: coverage, goroutines: goroutines, sender: sender, connection: connection}
}
