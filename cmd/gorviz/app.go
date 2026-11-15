package main

import (
	"context"
	"io"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/driver/httpserver"
	"github.com/kakkky/gorviz/driver/loginput"
	"github.com/kakkky/gorviz/driver/recorder"
	"github.com/kakkky/gorviz/presentation/terminal"
)

type ui interface {
	Run(ctx context.Context) error
}

// App は UI (web / top) と、その裏で動く録画ループ・ログの取り込みを束ねる
type App struct {
	ui       ui
	recorder *recorder.Recorder
	logInput *loginput.LogInput
}

func NewWebApp(srv *httpserver.HTTPServer, rec *recorder.Recorder, li *loginput.LogInput) *App {
	return &App{ui: srv, recorder: rec, logInput: li}
}

func NewTopApp(top *terminal.Top, rec *recorder.Recorder, li *loginput.LogInput) *App {
	return &App{ui: top, recorder: rec, logInput: li}
}

type logSource struct {
	r      io.Reader
	source string
	echo   io.Writer
}

// Run は UI が終わるまで動き、終わったら録画とログの取り込みも止める
func (a *App) Run(ctx context.Context, logs ...logSource) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(func(ctx context.Context) error {
			defer cancel()
			return a.ui.Run(ctx)
		})
		sc.Go(a.recorder.Run)
		for _, l := range logs {
			sc.Go(func(ctx context.Context) error {
				return a.logInput.Consume(ctx, l.r, l.source, l.echo)
			})
		}
		return nil
	})
}
