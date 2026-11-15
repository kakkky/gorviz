// Package zerologadapter は zerolog のログを gorviz に結ぶ。
//
//	logger := zerolog.New(zerologadapter.WrapWriter(os.Stderr)).Hook(zerologadapter.Hook())
package zerologadapter

import (
	"context"
	"io"

	"github.com/rs/zerolog"

	"github.com/kakkky/gorviz/internal/logmark"
)

// Hook はログ 1 行ごとに番号を振ってフィールドに足し、同じ番号を runtime/trace のログイベントとして書き込む。
// gorviz はこの番号でログ行と trace を突き合わせ、どの goroutine がいつそのログを書いたかを特定する
func Hook() zerolog.Hook {
	return hook{}
}

type hook struct{}

func (hook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	ctx := e.GetCtx()
	if ctx == nil {
		ctx = context.Background()
	}
	if n, ok := logmark.Mark(ctx); ok {
		e.Uint64(logmark.Key, n)
	}
}

// WrapWriter は w を包み、書かれたログ行を、アプリに埋め込んだ agent (gorviz.Start) にもそのまま渡す
func WrapWriter(w io.Writer) zerolog.LevelWriter {
	return agentWriter{w: w}
}

// agentWriter は LevelWriter も満たす。包んだせいで、レベルごとに書き分ける w の WriteLevel が呼ばれなくならないように
type agentWriter struct {
	w io.Writer
}

func (a agentWriter) Write(p []byte) (int, error) {
	n, err := a.w.Write(p)
	logmark.Forward(p)
	return n, err
}

func (a agentWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	lw, ok := a.w.(zerolog.LevelWriter)
	if !ok {
		return a.Write(p)
	}
	n, err := lw.WriteLevel(level, p)
	logmark.Forward(p)
	return n, err
}
