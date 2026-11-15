package gorviz

import (
	"context"
	"log/slog"

	"github.com/kakkky/gorviz/internal/logmark"
)

type slogHandler struct {
	inner slog.Handler
	// toAgent は埋め込んだ agent に渡す行を作る。agent が動いていないときは使わない
	toAgent slog.Handler
}

// NewSlogHandler は h を包み、ログ 1 行ごとに番号を振って属性に足し、同じ番号を runtime/trace のログイベントとして書き込む。
// gorviz はこの番号でログ行と trace を突き合わせ、どの goroutine がいつそのログを書いたかを特定する。
// Start で agent を埋め込んでいれば、ログをその agent にも渡す (Start より前に作った logger でもよい)
func NewSlogHandler(h slog.Handler) slog.Handler {
	return &slogHandler{inner: h, toAgent: slog.NewJSONHandler(agentSink{}, nil)}
}

func (h *slogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *slogHandler) Handle(ctx context.Context, r slog.Record) error {
	if n, ok := logmark.Mark(ctx); ok {
		r = r.Clone()
		r.AddAttrs(slog.Uint64(logmark.Key, n))
	}
	err := h.inner.Handle(ctx, r)
	if logmark.Forwarding() {
		_ = h.toAgent.Handle(ctx, r)
	}
	return err
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &slogHandler{inner: h.inner.WithAttrs(attrs), toAgent: h.toAgent.WithAttrs(attrs)}
}

func (h *slogHandler) WithGroup(name string) slog.Handler {
	return &slogHandler{inner: h.inner.WithGroup(name), toAgent: h.toAgent.WithGroup(name)}
}

// agentSink は JSON の 1 行を、埋め込んだ agent に渡す。
// 内側のハンドラが書いたバイト列は slog.Handler から取り出せないので、agent には JSON で書き直したものを渡す
type agentSink struct{}

func (agentSink) Write(p []byte) (int, error) {
	logmark.Forward(p)
	return len(p), nil
}
