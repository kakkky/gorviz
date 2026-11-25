// Package middleware は、アクセスログとエラーの返し方を handler の外でまとめて扱う
package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/kakkky/gorviz/example/todo-app/domain"
)

type errorSlot struct{ err error }

type slotKey struct{}

// SetError は handler がエラーを載せて即 return するための口
func SetError(ctx context.Context, err error) {
	if s, ok := ctx.Value(slotKey{}).(*errorSlot); ok {
		s.err = err
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Wrap は所要時間つきのアクセスログを出す。
// エラーは一覧の画面に戻して知らせる
func Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		slot := &errorSlot{}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		ctx := context.WithValue(r.Context(), slotKey{}, slot)
		defer func() {
			if p := recover(); p != nil {
				slot.err = domain.ErrInternal.With("panic")
				slog.ErrorContext(ctx, "panic", slog.Any("panic", p))
			}
			if slot.err != nil {
				writeError(ctx, rec, r, slot.err)
			}
			slog.InfoContext(ctx, "request done", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Int("status", rec.status), slog.Duration("duration", time.Since(start)))
		}()
		next.ServeHTTP(rec, r.WithContext(ctx))
	})
}

func writeError(ctx context.Context, w http.ResponseWriter, r *http.Request, err error) {
	msg := "something went wrong"
	var de *domain.Error
	if errors.As(err, &de) && !errors.Is(err, domain.ErrInternal) {
		msg = de.Message()
	} else {
		slog.ErrorContext(ctx, "request failed", slog.String("path", r.URL.Path), slog.Any("err", err))
		if errors.As(err, &de) {
			msg = de.Message() + " failed"
		}
	}
	http.Redirect(w, r, "/?error="+url.QueryEscape(msg), http.StatusSeeOther)
}
