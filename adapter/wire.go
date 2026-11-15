//go:build wireinject
// +build wireinject

package adapter

import (
	"github.com/google/wire"

	"github.com/kakkky/gorviz/adapter/client"
	"github.com/kakkky/gorviz/adapter/kvs"
	"github.com/kakkky/gorviz/adapter/logparse"
	"github.com/kakkky/gorviz/adapter/recording"
	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/usecase"
)

var Set = wire.NewSet(
	client.NewRuntimeClient,
	kvs.NewInMemoryKVS,
	recording.NewTraceStore,
	recording.NewLogStore,
	logparse.NewParser,
	usecase.NewUseCase,
	wire.Bind(new(domain.RuntimeClient), new(*client.RuntimeClient)),
	wire.Bind(new(domain.InMemoryKVS), new(*kvs.InMemoryKVS)),
	wire.Bind(new(domain.TraceRecording), new(*recording.TraceStore)),
	wire.Bind(new(domain.LogRecording), new(*recording.LogStore)),
	wire.Bind(new(domain.LogParser), new(*logparse.Parser)),
)
