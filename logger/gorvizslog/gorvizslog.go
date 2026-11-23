// Package gorvizslog は slog のログを gorviz に結ぶ。
//
//	logger := slog.New(gorvizslog.NewHandler(slog.NewJSONHandler(os.Stderr, nil)))
package gorvizslog

import (
	"context"
	"log/slog"

	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
)

// NewHandler は h を包み、ログ 1 行ごとに番号を振って属性に足し、同じ番号を runtime/trace のログイベントとして書き込む。
// アプリに agent を埋め込んでいれば (gorviz.Run)、ログをその agent にも渡す
func NewHandler(h slog.Handler) slog.Handler {
	return &handler{inner: h, toAgent: slog.NewJSONHandler(agentSink{}, &slog.HandlerOptions{Level: slog.LevelDebug})}
}

type handler struct {
	inner slog.Handler
	// toAgent は埋め込んだ agent に渡す行を作る。agent が動いていないときは使わない
	toAgent slog.Handler
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if n, ok := loggerhook.Mark(ctx); ok {
		r = r.Clone()
		r.AddAttrs(slog.Uint64(loggerhook.SeqKey, n))
	}
	err := h.inner.Handle(ctx, r)
	if loggerhook.Forwarding() {
		_ = h.toAgent.Handle(ctx, r)
	}
	return err
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{inner: h.inner.WithAttrs(attrs), toAgent: h.toAgent.WithAttrs(attrs)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name), toAgent: h.toAgent.WithGroup(name)}
}

// agentSink は JSON の 1 行を、埋め込んだ agent に渡す。包んだハンドラが書いたバイト列は slog.Handler から取り出せないので、JSON で書き直したものを渡す
type agentSink struct{}

func (agentSink) Write(p []byte) (int, error) {
	loggerhook.Forward(p)
	return len(p), nil
}
