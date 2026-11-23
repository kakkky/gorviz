package usecase

import "github.com/kakkky/gorviz/internal/app/agent/domain"

type UseCase struct {
	targets    domain.TargetRegistry
	states     domain.TargetStateStore
	memory     domain.GoroutineMemoryStore
	sequencer  domain.LogSequencer
	cursors    domain.CursorStore
	lifecycle  domain.InstanceLifecycle
	buffer     domain.ObservationBuffer
	runtime    domain.RuntimeClient
	kubernetes domain.KubernetesClient
	server     domain.ServerClient
}

func NewUseCase(
	targets domain.TargetRegistry,
	states domain.TargetStateStore,
	memory domain.GoroutineMemoryStore,
	sequencer domain.LogSequencer,
	cursors domain.CursorStore,
	lifecycle domain.InstanceLifecycle,
	buffer domain.ObservationBuffer,
	runtime domain.RuntimeClient,
	kubernetes domain.KubernetesClient,
	server domain.ServerClient,
) *UseCase {
	return &UseCase{
		targets:    targets,
		states:     states,
		memory:     memory,
		sequencer:  sequencer,
		cursors:    cursors,
		lifecycle:  lifecycle,
		buffer:     buffer,
		runtime:    runtime,
		kubernetes: kubernetes,
		server:     server,
	}
}
