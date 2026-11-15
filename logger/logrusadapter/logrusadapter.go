// Package logrusadapter は logrus のログを gorviz に結ぶ。
//
//	logger.SetFormatter(logrusadapter.WrapFormatter(&logrus.JSONFormatter{}))
package logrusadapter

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/kakkky/gorviz/internal/logmark"
)

// WrapFormatter は f を包み、ログ 1 行ごとに番号を振ってフィールドに足し、同じ番号を runtime/trace のログイベントとして書き込む。
// gorviz はこの番号でログ行と trace を突き合わせ、どの goroutine がいつそのログを書いたかを特定する。
// f が整形したログ行は、アプリに埋め込んだ agent (gorviz.Start) にもそのまま渡す。
// hook ではなく Formatter を包むのは、整形した後のバイト列を受け取れるのがここだけだから
func WrapFormatter(f logrus.Formatter) logrus.Formatter {
	return formatter{inner: f}
}

type formatter struct {
	inner logrus.Formatter
}

// Format の e は logrus がログ 1 行ごとに複製したものなので、Data に足しても呼び出し元の Entry は変わらない
func (f formatter) Format(e *logrus.Entry) ([]byte, error) {
	ctx := e.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if n, ok := logmark.Mark(ctx); ok {
		if e.Data == nil {
			e.Data = logrus.Fields{}
		}
		e.Data[logmark.Key] = n
	}
	b, err := f.inner.Format(e)
	if err == nil {
		logmark.Forward(b)
	}
	return b, err
}
