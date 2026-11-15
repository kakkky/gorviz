//go:build wireinject
// +build wireinject

package presentation

import (
	"github.com/google/wire"

	"github.com/kakkky/gorviz/presentation/handler"
	"github.com/kakkky/gorviz/presentation/terminal"
)

var GUISet = wire.NewSet(
	handler.NewHandler,
)

var CLISet = wire.NewSet(
	terminal.NewTop,
)
