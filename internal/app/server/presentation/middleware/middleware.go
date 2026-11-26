package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Middleware はアクセスログ・エラーの返し方・panic の復旧を受け持つ。/api/ は api の、それ以外は画面の ErrorWriter で書く
type Middleware struct {
	api ErrorWriter
	web ErrorWriter
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		start := time.Now()
		slot := &errorSlot{}
		r = r.WithContext(context.WithValue(r.Context(), errorKey{}, slot))
		rec := &recorder{ResponseWriter: rw}
		w := m.web
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w = m.api
		}
		defer func() {
			if p := recover(); p != nil {
				slot.err = domain.ErrInternal.With(fmt.Sprint("panic: ", p))
			}
			if slot.err != nil {
				status := StatusOf(slot.err)
				if status == http.StatusInternalServerError {
					slog.ErrorContext(r.Context(), "request failed", slog.String("path", r.URL.Path), slog.Any("err", slot.err))
				}
				if rec.status == 0 {
					w.WriteError(rec, r, status, slot.err)
				}
			}
			slog.InfoContext(r.Context(), "request", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Int("status", rec.status), slog.Duration("took", time.Since(start)))
		}()
		next.ServeHTTP(rec, r)
	})
}
