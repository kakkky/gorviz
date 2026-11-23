// Package middleware は HTTP (GUI・HTTP API) の手前で、認証・アクセスログ・エラーの返し方・panic の復旧をまとめて扱う
package middleware

import (
	"context"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/presentation/response"
)

// SessionCookie はブラウザのログインを持つ cookie の名前
const SessionCookie = "gorviz_session"

// TokenChecker は HTTP API・GUI のトークンが有効か
type TokenChecker interface {
	ValidAPIToken(token string) bool
}

type Middleware struct {
	tokens TokenChecker
}

func NewMiddleware(tokens TokenChecker) *Middleware {
	return &Middleware{tokens: tokens}
}

type errorHolder struct{ err error }

type holderKey struct{}

// SetError は handler がエラーをリクエストに載せる。書き出しは middleware が defer で行う
func SetError(r *http.Request, err error) {
	if h, ok := r.Context().Value(holderKey{}).(*errorHolder); ok {
		h.err = err
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func isAPI(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/api/") }

// Handle は panic の復旧・アクセスログ・エラーの書き出しを next に被せる。すべての経路の一番外側に置く
func (m *Middleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: rw}
		holder := &errorHolder{}
		r = r.WithContext(context.WithValue(r.Context(), holderKey{}, holder))
		defer func() {
			if p := recover(); p != nil {
				slog.ErrorContext(r.Context(), "panic in an http handler", slog.Any("panic", p), slog.String("stack", string(debug.Stack())))
				holder.err = domain.ErrInternal.With("panic")
			}
			if holder.err != nil && rec.status == 0 {
				writeError(rec, r, holder.err)
			}
			attrs := []any{slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Int("status", rec.status), slog.Duration("took", time.Since(start))}
			if rec.status >= 500 {
				slog.ErrorContext(r.Context(), "http request", append(attrs, slog.Any("err", holder.err))...)
			} else {
				slog.InfoContext(r.Context(), "http request", attrs...)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, errUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, domain.ErrInvalidArgument):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

type apiError struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Status     int      `json:"status"`
	Kind       string   `json:"kind"`
	Message    string   `json:"message"`
	Candidates []string `json:"candidates,omitempty"`
	Hint       string   `json:"hint,omitempty"`
}

func writeError(rw http.ResponseWriter, r *http.Request, err error) {
	status := statusOf(err)
	body := errorBody{Status: status, Kind: kindOf(status), Message: domain.Message(err), Candidates: domain.CandidatesOf(err)}
	if status == http.StatusUnauthorized {
		body.Message, body.Hint = "the token is missing or not valid", "pass a token issued by gorviz-server as Authorization: Bearer <token> (gorviz-cli and gorviz-mcp read it from GORVIZ_TOKEN)"
	}
	if isAPI(r) {
		if werr := response.Write(rw, r, status, apiError{Error: body}); werr != nil {
			slog.ErrorContext(r.Context(), "write an error response", slog.Any("err", werr))
		}
		return
	}
	// GUI のエラーの画面ができるまでの最小の画面
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(status)
	rw.Write([]byte("<!doctype html><title>gorviz</title><h1>" + http.StatusText(status) + "</h1><p>" + html.EscapeString(body.Message) + "</p>"))
}

func kindOf(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusBadRequest:
		return "invalid-argument"
	case http.StatusNotFound:
		return "not-found"
	}
	return "internal"
}

var errUnauthorized = errors.New("unauthorized")

// RequireToken は HTTP API の Authorization: Bearer のトークンを確かめる
func (m *Middleware) RequireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !m.tokens.ValidAPIToken(strings.TrimSpace(token)) {
			SetError(r, errUnauthorized)
			return
		}
		next.ServeHTTP(rw, r)
	})
}

// RequireLogin は GUI のログイン (cookie) を確かめる。なければログインの画面へ移す
func (m *Middleware) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(SessionCookie); err == nil && m.tokens.ValidAPIToken(c.Value) {
			next.ServeHTTP(rw, r)
			return
		}
		http.Redirect(rw, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
	})
}
