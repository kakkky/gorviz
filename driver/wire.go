//go:build wireinject
// +build wireinject

package driver

import (
	"github.com/google/wire"

	"github.com/kakkky/gorviz/driver/httpserver"
	"github.com/kakkky/gorviz/driver/loginput"
	"github.com/kakkky/gorviz/driver/recorder"
)

var Set = wire.NewSet(
	recorder.NewRecorder,
	loginput.NewLogInput,
)

var HTTPSet = wire.NewSet(
	httpserver.NewMux,
	httpserver.NewHTTPServer,
)
