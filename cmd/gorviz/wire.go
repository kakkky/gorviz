//go:build wireinject
// +build wireinject

package main

import (
	"github.com/google/wire"

	"github.com/kakkky/gorviz/adapter"
	"github.com/kakkky/gorviz/adapter/schedtrace"
	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
	"github.com/kakkky/gorviz/driver"
	"github.com/kakkky/gorviz/presentation"
)

// schedtrace.Store は gorviz run のときだけ子プロセスの出力で埋まるので、Set の provider ではなく引数で受け取る
var schedTraceBind = wire.Bind(new(domain.SchedTraceSource), new(*schedtrace.Store))

func InitWebApp(cfg *config.Config, schedTrace *schedtrace.Store) *App {
	wire.Build(
		driver.Set,
		driver.HTTPSet,
		adapter.Set,
		presentation.GUISet,
		schedTraceBind,
		NewWebApp,
	)
	return nil
}

func InitTopApp(cfg *config.Config, schedTrace *schedtrace.Store) *App {
	wire.Build(
		driver.Set,
		adapter.Set,
		presentation.CLISet,
		schedTraceBind,
		NewTopApp,
	)
	return nil
}
