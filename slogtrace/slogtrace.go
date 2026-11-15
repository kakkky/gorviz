// Package slogtrace は、観測対象のアプリが slog のハンドラを包むためのパッケージ。
// ログ 1 行ごとに番号を振って属性に足し、同じ番号を runtime/trace のログイベントとして書き込む。
// gorviz はこの番号でログ行と trace を突き合わせ、どの goroutine がいつそのログを書いたかを特定する。
//
//	logger := slog.New(slogtrace.Wrap(slog.NewJSONHandler(os.Stderr, nil)))
package slogtrace

import (
	"context"
	"log/slog"
	"runtime/trace"
	"strconv"
	"sync/atomic"
)

const (
	// TraceCategory は trace.Log に渡すカテゴリ
	TraceCategory = "gorviz.log"
	// SeqAttrKey はログ行に足す属性のキー
	SeqAttrKey = "gorviz_seq"
)

var seq atomic.Uint64

type handler struct {
	inner slog.Handler
}

func Wrap(h slog.Handler) slog.Handler {
	return &handler{inner: h}
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle は trace が有効なとき (gorviz が録画している間) だけ番号を振る。
// trace.Log は呼んだ goroutine の上でイベントになるので、ログを書いた goroutine がそのまま記録される。
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if trace.IsEnabled() {
		n := seq.Add(1)
		r = r.Clone()
		r.AddAttrs(slog.Uint64(SeqAttrKey, n))
		trace.Log(ctx, TraceCategory, strconv.FormatUint(n, 10))
	}
	return h.inner.Handle(ctx, r)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{inner: h.inner.WithAttrs(attrs)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name)}
}
